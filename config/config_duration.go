package config

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/kr/pretty"
	"github.com/zrepl/yaml-config"
)

type Duration struct{ d time.Duration }

func (d Duration) Duration() time.Duration { return d.d }

var _ yaml.Unmarshaler = &Duration{}

func (d *Duration) UnmarshalYAML(unmarshal func(v interface{}, not_strict bool) error) error {
	var s string
	err := unmarshal(&s, false)
	if err != nil {
		return err
	}
	d.d, err = parseDuration(s)
	if err != nil {
		d.d = 0
		return &yaml.TypeError{Errors: []string{fmt.Sprintf("cannot parse value %q: %s", s, err)}}
	}
	return nil
}

type PositiveDuration struct{ d Duration }

var _ yaml.Unmarshaler = &PositiveDuration{}

func (d PositiveDuration) Duration() time.Duration { return d.d.Duration() }

func (d *PositiveDuration) UnmarshalYAML(unmarshal func(v interface{}, not_strict bool) error) error {
	err := d.d.UnmarshalYAML(unmarshal)
	if err != nil {
		return err
	}
	if d.d.Duration() <= 0 {
		return fmt.Errorf("duration must be positive, got %s", d.d.Duration())
	}
	return nil
}

func parsePositiveDuration(e string) (time.Duration, error) {
	d, err := parseDuration(e)
	if err != nil {
		return d, err
	}
	if d <= 0 {
		return 0, errors.New("duration must be positive integer")
	}
	return d, err
}

var durationStringRegex *regexp.Regexp = regexp.MustCompile(`^\s*([\+-]?\d+)\s*(|s|m|h|d|w)\s*$`)

func parseDuration(e string) (d time.Duration, err error) {
	comps := durationStringRegex.FindStringSubmatch(e)
	if comps == nil {
		err = fmt.Errorf("must match %s", durationStringRegex)
		return
	}
	if len(comps) != 3 {
		panic(pretty.Sprint(comps))
	}

	durationFactor, err := strconv.ParseInt(comps[1], 10, 64)
	if err != nil {
		return 0, err
	}

	var durationUnit time.Duration
	switch comps[2] {
	case "":
		if durationFactor != 0 {
			err = fmt.Errorf("missing time unit")
			return
		} else {
			// It's the case where user specified '0'.
			// We want to allow this, just like time.ParseDuration.
		}
	case "s":
		durationUnit = time.Second
	case "m":
		durationUnit = time.Minute
	case "h":
		durationUnit = time.Hour
	case "d":
		durationUnit = 24 * time.Hour
	case "w":
		durationUnit = 24 * 7 * time.Hour
	default:
		err = fmt.Errorf("contains unknown time unit '%s'", comps[2])
		return
	}

	d = time.Duration(durationFactor) * durationUnit
	return
}
