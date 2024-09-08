package snapname

import (
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/daemon/snapper/snapname/timestamp"
	"github.com/zrepl/zrepl/zfs"
)

type Formatter struct {
	prefix    string
	timestamp *timestamp.Formatter
}

func New(prefix, tsFormat, tsLocation string) (*Formatter, error) {
	timestamp, err := timestamp.New(tsFormat, tsLocation)
	if err != nil {
		return nil, err
	}
	formatter := &Formatter{
		prefix:    prefix,
		timestamp: timestamp,
	}
	// Best-effort check to detect whether the result would be an invalid name.
	// Test two dates that in most places have will have different time zone offsets due to DST.
	check := func(t time.Time) error {
		testFormat := "some/dataset@" + formatter.Format(t)
		if err := zfs.EntityNamecheck(testFormat, zfs.EntityTypeSnapshot); err != nil {
			return errors.Wrapf(err, "prefix or timestamp format result in invalid snapshot name such as %q", testFormat)
		}
		return nil
	}
	if err := check(time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		return nil, err
	}
	if err := check(time.Date(2020, 12, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		return nil, err
	}
	return formatter, nil
}

func (f *Formatter) Format(now time.Time) string {
	return f.prefix + f.timestamp.Format(now)
}

func (f *Formatter) Prefix() string {
	return f.prefix
}
