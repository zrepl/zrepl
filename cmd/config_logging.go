package cmd

import (
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	//"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
)

type LoggingConfig struct {
	Stdout struct {
		Level logrus.Level
	}
	//LFS lfshook.PathMap
}

func parseLogging(i interface{}) (c *LoggingConfig, err error) {

	c = &LoggingConfig{}
	c.Stdout.Level = logrus.WarnLevel
	if i == nil {
		return c, nil
	}

	var asMap struct {
		Mate   string
		Stdout map[string]string
		LFS    map[string]string
	}
	if err = mapstructure.Decode(i, &asMap); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	//if asMap.LFS != nil {
	//	c.LFS = make(map[logrus.Level]string, len(asMap.LFS))
	//	for level_str, path := range asMap.LFS {
	//		level, err := logrus.ParseLevel(level_str)
	//		if err != nil {
	//			return nil, errors.Wrapf(err, "cannot parse level '%s'", level_str)
	//		}
	//		if len(path) <= 0 {
	//			return nil, errors.Errorf("path must be longer than 0")
	//		}
	//		c.LFS[level] = path
	//	}
	//}

	if asMap.Stdout != nil {
		lvl, err := logrus.ParseLevel(asMap.Stdout["level"])
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse stdout log level")
		}
		c.Stdout.Level = lvl
	}

	return c, nil

}

func (c *LoggingConfig) MakeLogrus() (l logrus.FieldLogger) {

	log := logrus.New()
	log.Out = nopWriter(0)
	log.Level = logrus.DebugLevel

	//log.Level = logrus.DebugLevel
	//
	//if len(c.LFS) > 0 {
	//	lfshook := lfshook.NewHook(c.LFS)
	//	log.Hooks.Add(lfshook)
	//}

	stdhook := NewStdHook()
	log.Hooks.Add(stdhook)

	return log

}
