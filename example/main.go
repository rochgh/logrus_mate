package main

import (
	"fmt"

	"github.com/gogap/errors"
	"github.com/gogap/logrus_mate"
	"github.com/sirupsen/logrus"

	_ "github.com/gogap/logrus_mate/hooks/expander"
	_ "github.com/gogap/logrus_mate/hooks/file"
)

func main() {
	// Hijack logrus StandardLogger()
	logrus_mate.Hijack(logrus.StandardLogger(), logrus_mate.ConfigString(`{formatter.name = "json"}`))

	logrus.WithField("Field", "A").Debugln("Hello JSON")

	mate, err := logrus_mate.NewLogrusMate(logrus_mate.ConfigFile("mate.conf"))

	newLoger := logrus.New()

	if err = mate.Hijack(newLoger, "mike"); err != nil {
		fmt.Println(err)
		return
	}

	// newLogger is Hijackt by mike config
	newLoger.Debug("You could not see me")
	newLoger.Errorln("Hello Error Level")

	mikeLoger := mate.Logger("mike")

	// create a new mike logger
	mikeLoger.Errorln("Hello Error Level from Mike")

	ErrGoGapError := errors.TN("LogrusMate", 1000, "hello gogap {{.name}}")

	e := ErrGoGapError.New(errors.Params{"name": "zeal"})

	// it will hijack
	mikeLoger.WithError(e).Errorln("hello with gogap errors")
}
