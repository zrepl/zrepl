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
	// Best-effort check to detect whether the result would be an invalid name early.
	testFormat := "some/dataset@" + formatter.Format(time.Now())
	if err := zfs.EntityNamecheck(testFormat, zfs.EntityTypeSnapshot); err != nil {
		return nil, errors.Wrapf(err, "prefix or timestamp format result in invalid snapshot name such as %q", testFormat)
	}
	return formatter, nil
}

func (f *Formatter) Format(now time.Time) string {
	return f.prefix + f.timestamp.Format(now)
}

func (f *Formatter) Prefix() string {
	return f.prefix
}
