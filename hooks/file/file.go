package logrus_file

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RFC5424 log message levels.
const (
	LevelError = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

const (
	y1  = `0123456789`
	y2  = `0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789`
	y3  = `0000000000111111111122222222223333333333444444444455555555556666666666777777777788888888889999999999`
	y4  = `0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789`
	mo1 = `000000000111`
	mo2 = `123456789012`
	d1  = `0000000001111111111222222222233`
	d2  = `1234567890123456789012345678901`
	h1  = `000000000011111111112222`
	h2  = `012345678901234567890123`
	mi1 = `000000000011111111112222222222333333333344444444445555555555`
	mi2 = `012345678901234567890123456789012345678901234567890123456789`
	s1  = `000000000011111111112222222222333333333344444444445555555555`
	s2  = `012345678901234567890123456789012345678901234567890123456789`
	ns1 = `0123456789`
)

// fileLogWriter implements LoggerInterface.
// It writes messages by lines limit, file size limit, or time frequency.
type fileLogWriter struct {
	sync.RWMutex // write log order by order and  atomic incr maxLinesCurLines and maxSizeCurSize
	// The opened file
	Filename   string `json:"filename"`
	fileWriter *os.File

	// Rotate at line
	MaxLines         int `json:"maxlines"`
	maxLinesCurLines int

	// Rotate at size
	MaxSize        int `json:"maxsize"`
	maxSizeCurSize int

	StripColors bool `json:"stripcolors"`

	Hourly         bool `json:"hourly"`
	HourlyOpenDate int  `json:"hourly_open"`

	// Rotate daily
	Daily         bool  `json:"daily"`
	MaxDays       int64 `json:"maxdays"`
	DailyOpenDate int   `json:"daily_open"`
	dailyOpenTime time.Time

	Rotate bool `json:"rotate"`

	Level int `json:"level"`

	Perm string `json:"perm"`

	RotatePerm string `json:"rotateperm"`

	fileNameOnly, suffix string // like "project.log", project is fileNameOnly and .log is suffix
}

var instance map[string]*fileLogWriter

// newFileWriter create a FileLogWriter returning as LoggerInterface.
func newFileWriter(jsonConfig string) *fileLogWriter {

	if instance == nil {
		instance = make(map[string]*fileLogWriter)
	}

	if value, ok := instance[jsonConfig]; ok {
		_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: newFileWriter use exist %v\n", GoId(), time.Now(), value)
		return value
	}

	w := &fileLogWriter{
		StripColors: true,
		Daily:       true,
		Hourly:      true,
		MaxDays:     7,
		Rotate:      true,
		RotatePerm:  "0440",
		Level:       LevelDebug,
		Perm:        "0660",
	}

	_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: newFileWriter create new %v\n", GoId(), time.Now(), w)

	err := w.Init(jsonConfig)
	if err != nil {
		return nil
	}

	instance[jsonConfig] = w

	return w
}

func (w fileLogWriter) String() string {

	b, err := json.Marshal(w)
	if err != nil {
		return fmt.Sprintf("%s, %d, %d", w.Filename, w.HourlyOpenDate, w.DailyOpenDate)
	}

	return string(b)
}

// Init file logger with json config.
// jsonConfig like:
//	{
//	"filename":"logs/beego.log",
//	"maxLines":10000,
//	"maxsize":1024,
//	"daily":true,
//	"hourly":true,
//	"maxDays":15,
//	"rotate":true,
//  	"perm":"0600"
//	}
func (w *fileLogWriter) Init(jsonConfig string) error {
	err := json.Unmarshal([]byte(jsonConfig), w)
	if err != nil {
		return err
	}
	if len(w.Filename) == 0 {
		return errors.New("jsonconfig must have filename")
	}
	w.suffix = filepath.Ext(w.Filename)
	w.fileNameOnly = strings.TrimSuffix(w.Filename, w.suffix)
	if w.suffix == "" {
		w.suffix = ".log"
	}
	err = w.startLogger()
	return err
}

// start file logger. create log file and set to locker-inside file writer.
func (w *fileLogWriter) startLogger() error {
	file, err := w.createLogFile()
	if err != nil {
		return err
	}
	if w.fileWriter != nil {
		_ = w.fileWriter.Close()
	}
	w.fileWriter = file
	return w.initFd()
}

func (w *fileLogWriter) needRotate(size int, day int, hour int) bool {

	return (w.MaxLines > 0 && w.maxLinesCurLines >= w.MaxLines) ||
		(w.MaxSize > 0 && w.maxSizeCurSize >= w.MaxSize) ||
		(w.Daily && day != w.DailyOpenDate) ||
		(w.Hourly && hour != w.HourlyOpenDate)
}

const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"

var re = regexp.MustCompile(ansi)

func Strip(str string) string {
	return re.ReplaceAllString(str, "")
}

func GoId() int {
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("panic recover:panic info: %v\n", err)
		}
	}()

	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, err := strconv.Atoi(idField)
	if err != nil {
		panic(fmt.Sprintf("cannot get goroutine id: %v", err))
	}
	return id
}

// WriteMsg write logger message into file.
func (w *fileLogWriter) WriteMsg(when time.Time, msg string) error {
	_, d, h := formatTimeHeader(when)

	if w.StripColors {
		msg = Strip(msg)
	}

	if w.Rotate {
		w.RLock()
		if w.needRotate(len(msg), d, h) {
			w.RUnlock()
			w.Lock()

			_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: WriteMsg day %d, hour %d, %v\n", GoId(), time.Now(), d, h, w)

			if w.needRotate(len(msg), d, h) {
				if err := w.doRotate(when); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "%d %v WriteMsg FileLogWriter(%q): %s\n", GoId(), when, w.Filename, err)
				}
			}

			w.Unlock()
		} else {
			w.RUnlock()
		}
	}

	w.Lock()
	_, err := w.fileWriter.Write([]byte(msg))
	if err == nil {
		w.maxLinesCurLines++
		w.maxSizeCurSize += len(msg)
	}
	w.Unlock()

	return err
}

func (w *fileLogWriter) createLogFile() (*os.File, error) {
	// Open the log file
	perm, err := strconv.ParseInt(w.Perm, 8, 64)
	if err != nil {
		return nil, err
	}

	fd, err := os.OpenFile(w.Filename, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.FileMode(perm))
	if err == nil {
		// Make sure file perm is user set perm cause of `os.OpenFile` will obey umask
		_ = os.Chmod(w.Filename, os.FileMode(perm))
	}
	return fd, err
}

func (w *fileLogWriter) initFd() error {
	fd := w.fileWriter
	fInfo, err := fd.Stat()
	if err != nil {
		return fmt.Errorf("get stat err: %s", err)
	}

	w.maxSizeCurSize = int(fInfo.Size())
	w.dailyOpenTime = fInfo.ModTime()
	w.DailyOpenDate = w.dailyOpenTime.Day()
	w.HourlyOpenDate = w.dailyOpenTime.Hour()
	w.maxLinesCurLines = 0
	if w.Rotate {
		if fInfo.Size() > 0 && w.MaxLines > 0 {
			count, err := w.lines()
			if err != nil {
				return err
			}
			w.maxLinesCurLines = count
		}
	}
	return nil
}

func (w *fileLogWriter) lines() (int, error) {
	fd, err := os.Open(w.Filename)
	if err != nil {
		return 0, err
	}
	defer fd.Close()

	buf := make([]byte, 32768) // 32k
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := fd.Read(buf)
		if err != nil && err != io.EOF {
			return count, err
		}

		count += bytes.Count(buf[:c], lineSep)

		if err == io.EOF {
			break
		}
	}

	return count, nil
}

// DoRotate means it need to write file in new file.
// new file name like xx.2013-01-01.log (daily) or xx.001.log (by line or size)
func (w *fileLogWriter) doRotate(logTime time.Time) error {
	_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: doRotate logTime %v, %v\n", GoId(), time.Now(), logTime, w)

	// file exists
	// Find the next available number
	maxSuffixNum := 999
	num := 1
	fName := ""
	rotatePerm, err := strconv.ParseInt(w.RotatePerm, 8, 64)
	if err != nil {
		return err
	}

	timeFormat := "2006-01-02"
	if w.Hourly {
		timeFormat = "2006-01-02-15"
	}

	_, err = os.Lstat(w.Filename)
	if err != nil {
		//even if the file is not exist or other, we should RESTART the logger
		return w.restartLogger(err)
	}

	for ; err == nil && num <= maxSuffixNum; num++ {
		fName = fmt.Sprintf("%s.%s.%03d%s", w.fileNameOnly, w.dailyOpenTime.Format(timeFormat), num, w.suffix)
		_, err = os.Lstat(fName)
		// if file exist, try next
		if err == nil {
			_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: file exist %s, %v\n", GoId(), time.Now(), fName, w)
			continue
		}

		// for the fist log, we don't want the num suffix
		if num == 1 {
			withoutNumName := fmt.Sprintf("%s.%s%s", w.fileNameOnly, w.dailyOpenTime.Format(timeFormat), w.suffix)
			_, err = os.Lstat(withoutNumName)
			if err == nil {

				if w.MaxLines == 0 && w.MaxSize == 0 {
					// skip rotate file, dest file exist and new message come. do nothing, write to current file.
					_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: skip rotate file %s, %v\n", GoId(), time.Now(), withoutNumName, w)
					return w.restartLogger(err)
				}

				err = os.Rename(withoutNumName, fName)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: Rename %s to %s failed, %v\n", GoId(), time.Now(), withoutNumName, fName, err)
				}
				_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: Rename %s to %s ok, %v\n", GoId(), time.Now(), withoutNumName, fName, w)
			} else {
				fName = withoutNumName
				_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: use file name %s, %v\n", GoId(), time.Now(), fName, w)
				break
			}
		}
	}

	// return error if the last file checked still existed
	if err == nil && num > maxSuffixNum {
		return fmt.Errorf("rotate: Cannot find free log number to rename %s", w.Filename)
	}

	// close fileWriter before rename
	w.fileWriter.Close()

	_, _ = fmt.Fprintf(os.Stderr, "%d %v rotate: Rename log %s to %s ok, %v\n", GoId(), time.Now(), w.Filename, fName, w)

	// Rename the file to its new found name
	// even if occurs error,we MUST guarantee to restart new logger
	err = os.Rename(w.Filename, fName)
	if err != nil {
		return w.restartLogger(err)
	}

	err = os.Chmod(fName, os.FileMode(rotatePerm))

	return w.restartLogger(err)
}

func (w *fileLogWriter) restartLogger(err error) error {

	startLoggerErr := w.startLogger()
	go w.deleteOldLog()

	if startLoggerErr != nil {
		return fmt.Errorf("rotate: restartLogger startLoggerErr: %v", startLoggerErr)
	}

	if err != nil {
		return fmt.Errorf("rotate: restartLogger err: %v", err)
	}

	return nil
}

func (w *fileLogWriter) deleteOldLog() {
	dir := filepath.Dir(w.Filename)
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) (returnErr error) {
		defer func() {
			if r := recover(); r != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Unable to delete old log '%s', error: %v\n", path, r)
			}
		}()

		if info == nil {
			return
		}

		if !info.IsDir() && info.ModTime().Add(24 * time.Hour * time.Duration(w.MaxDays)).Before(time.Now()) {
			if strings.HasPrefix(filepath.Base(path), filepath.Base(w.fileNameOnly)) &&
				strings.HasSuffix(filepath.Base(path), w.suffix) {
				_ = os.Remove(path)
			}
		}
		return
	})
}

// Destroy close the file description, close file writer.
func (w *fileLogWriter) Destroy() {
	w.fileWriter.Close()
}

// Flush flush file logger.
// there are no buffering messages in file logger in memory.
// flush file means sync file from disk.
func (w *fileLogWriter) Flush() {
	_ = w.fileWriter.Sync()
}

func formatTimeHeader(when time.Time) ([]byte, int, int) {
	y, mo, d := when.Date()
	h, mi, s := when.Clock()
	ns := when.Nanosecond() / 1000000
	//len("2006/01/02 15:04:05.123 ")==24
	var buf [24]byte

	buf[0] = y1[y/1000%10]
	buf[1] = y2[y/100]
	buf[2] = y3[y-y/100*100]
	buf[3] = y4[y-y/100*100]
	buf[4] = '/'
	buf[5] = mo1[mo-1]
	buf[6] = mo2[mo-1]
	buf[7] = '/'
	buf[8] = d1[d-1]
	buf[9] = d2[d-1]
	buf[10] = ' '
	buf[11] = h1[h]
	buf[12] = h2[h]
	buf[13] = ':'
	buf[14] = mi1[mi]
	buf[15] = mi2[mi]
	buf[16] = ':'
	buf[17] = s1[s]
	buf[18] = s2[s]
	buf[19] = '.'
	buf[20] = ns1[ns/100]
	buf[21] = ns1[ns%100/10]
	buf[22] = ns1[ns%10]

	buf[23] = ' '

	return buf[0:], d, h
}
