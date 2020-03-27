package zfscmd

import (
	"time"
)

// Implementation Note:
//
// Pre-events logged with debug
// Post-event without error logged with info
// Post-events with error logged at error level

func startPreLogging(c *Cmd, now time.Time) {
	c.log().Debug("starting command")
}

func startPostLogging(c *Cmd, err error, now time.Time) {
	if err == nil {
		c.log().Info("started command")
	} else {
		c.log().WithError(err).Error("cannot start command")
	}
}

func waitPreLogging(c *Cmd, now time.Time) {
	c.log().Debug("start waiting")
}

func waitPostLogging(c *Cmd, err error, now time.Time) {
	log := c.log().
		WithField("total_time_s", c.Runtime().Seconds()).
		WithField("systemtime_s", c.cmd.ProcessState.SystemTime().Seconds()).
		WithField("usertime_s", c.cmd.ProcessState.UserTime().Seconds())

	if err == nil {
		log.Info("command exited without error")
	} else {
		log.WithError(err).Error("command exited with error")
	}
}
