package timestamp_formatting

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type Formatter struct {
	format   func(time.Time) string
	location *time.Location
}

func New(formatString string, locationString string) (*Formatter, error) {
	location, err := time.LoadLocation(locationString) // no shadow
	if err != nil {
		return nil, errors.Wrapf(err, "load location from string %q", locationString)
	}
	makeFormatFunc := func(formatString string) (func(time.Time) string, error) {
		if strings.Contains(formatString, "+") {
			return nil, fmt.Errorf("character '+' is not allowed in ZFS snapshot names")
		}
		return func(t time.Time) string {
			res := t.Format(formatString)
			// if the formatString contains a time zone specifier
			// and the location would result in a positive offset to UTC
			// then the result of t.Format would contain a '+' sign.
			if isLocationPositiveOffsetToUTC(location) {
				// the only source of `+` can be the positive time zone offset because we disallowed `+` as a character in the format string
				res = strings.Replace(res, "+", "_", 1)
			}
			if strings.Contains(res, "+") {
				panic(fmt.Sprintf("format produced a string containing illegal character '+' that wasn't the expected case of positive time zone offset: format=%q location=%q unix=%q result=%q", formatString, location, t.Unix(), res))
			}
			return res
		}, nil
	}
	var formatFunc func(time.Time) string
	mustUseUtcError := func() error {
		return fmt.Errorf("format string requires UTC location")
	}
	switch strings.ToLower(formatString) {
	case "dense":
		if location != time.UTC {
			err = mustUseUtcError()
		} else {
			formatFunc, err = makeFormatFunc("20060102_150405_000")
		}
	case "human":
		if location != time.UTC {
			err = mustUseUtcError()
		} else {
			formatFunc, err = makeFormatFunc("2006-01-02_15:04:05")
		}
	case "iso-8601":
		formatFunc, err = makeFormatFunc("2006-01-02T15:04:05.000Z0700")
	case "unix-seconds":
		if location != time.UTC {
			// Technically not required because unix time is by definition in UTC
			// but let's make that clear to confused users...
			err = mustUseUtcError()
		} else {
			formatFunc = func(t time.Time) string {
				return strconv.FormatInt(t.Unix(), 10)
			}
		}
	default:
		formatFunc, err = makeFormatFunc(formatString)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "invalid format string %q or location %q", formatString, locationString)
	}
	return &Formatter{
		format:   formatFunc,
		location: location,
	}, nil
}

func isLocationPositiveOffsetToUTC(location *time.Location) bool {
	_, offsetSeconds := time.Now().In(location).Zone()
	return offsetSeconds > 0
}

func (f *Formatter) Format(t time.Time) string {
	if f.location != nil {
		t = t.In(f.location)
	}
	return f.format(t)
}
