package logrus_file

import (
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"time"

	"github.com/gogap/config"
	"github.com/gogap/logrus_mate"
)

type fileHookConfig struct {
	Filename    string `json:"filename"`
	MaxLines    int64  `json:"maxLines"`
	MaxSize     int64  `json:"maxsize"`
	StripColors bool   `json:"stripColors"`
	Daily       bool   `json:"daily"`
	Hourly      bool   `json:"hourly"`
	MaxDays     int64  `json:"maxDays"`
	Rotate      bool   `json:"rotate"`
	Perm        string `json:"perm"`
	RotatePerm  string `json:"rotateperm"`
	Level       int32  `json:"level"`
}

func init() {
	logrus_mate.RegisterHook("file", NewFileHook)
}

func NewFileHook(config config.Configuration) (hook logrus.Hook, err error) {

	filename := config.GetString("filename", "logs/logrus.log")

	dir := filepath.Dir(filename)

	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return
	}

	hookConf := fileHookConfig{
		Filename:    filename,
		StripColors: config.GetBoolean("strip-colors", true),
		Daily:       config.GetBoolean("daily", true),
		Hourly:      config.GetBoolean("hourly", true),
		MaxDays:     config.GetInt64("max-days", 7),
		Rotate:      config.GetBoolean("rotate", true),
		MaxLines:    config.GetInt64("max-lines", 10000),
		MaxSize:     config.GetInt64("max-size", 1024),
		RotatePerm:  config.GetString("rotate-perm", "0440"),
		Perm:        config.GetString("perm", "0660"),
		Level:       config.GetInt32("level"),
	}

	confData, err := json.Marshal(hookConf)
	if err != nil {
		return
	}

	w := newFileWriter(string(confData))
	if w == nil {
		return
	}

	hook = &FileHook{W: w}

	return
}

type FileHook struct {
	W *fileLogWriter
}

func (p *FileHook) Fire(entry *logrus.Entry) (err error) {
	if p.W.Level < int(entry.Level) {
		return nil
	}
	message, err := entry.String()

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unable to read entry, %v", err)
		return err
	}

	now := time.Now()

	return p.W.WriteMsg(now, message)
}

func (p *FileHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
		logrus.DebugLevel,
	}
}
