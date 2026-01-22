package zfscmd

import (
	"time"
)

// Implementation Note:
//
// Pre-events logged with debug
// Post-event without error logged with info
// Post-events with error _also_ logged with info
// (Not all errors we observe at this layer) are actual errors in higher-level layers)

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

func waitPostLogging(c *Cmd, u usage, err error, now time.Time) {

	log := c.log().
		WithField("total_time_s", u.total_secs).
		WithField("systemtime_s", u.system_secs).
		WithField("usertime_s", u.user_secs)

	if err == nil {
		log.Info("command exited with success")
	} else {
		log.WithError(err).Info("command exited with error")
	}
}
