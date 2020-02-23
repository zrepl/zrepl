package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type RetentionIntervalList []RetentionInterval

type PruneGrid struct {
	Type  string                `yaml:"type"`
	Grid  RetentionIntervalList `yaml:"grid"`
	Regex string                `yaml:"regex"`
}

type RetentionInterval struct {
	length    time.Duration
	keepCount int
}

func (i *RetentionInterval) Length() time.Duration {
	return i.length
}

func (i *RetentionInterval) KeepCount() int {
	return i.keepCount
}

const RetentionGridKeepCountAll int = -1

func (t *RetentionIntervalList) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	var in string
	if err := u(&in, true); err != nil {
		return err
	}

	intervals, err := parseRetentionGridIntervalsString(in)
	if err != nil {
		return err
	}

	*t = intervals

	return nil
}

var retentionStringIntervalRegex *regexp.Regexp = regexp.MustCompile(`^\s*(\d+)\s*x\s*([^\(]+)\s*(\((.*)\))?\s*$`)

func parseRetentionGridIntervalString(e string) (intervals []RetentionInterval, err error) {

	comps := retentionStringIntervalRegex.FindStringSubmatch(e)
	if comps == nil {
		err = fmt.Errorf("retention string does not match expected format")
		return
	}

	times, err := strconv.Atoi(comps[1])
	if err != nil {
		return nil, err
	} else if times <= 0 {
		return nil, fmt.Errorf("contains factor <= 0")
	}

	duration, err := parsePositiveDuration(comps[2])
	if err != nil {
		return nil, err
	}

	keepCount := 1
	if comps[3] != "" {
		// Decompose key=value, comma separated
		// For now, only keep_count is supported
		re := regexp.MustCompile(`^\s*keep=(.+)\s*$`)
		res := re.FindStringSubmatch(comps[4])
		if res == nil || len(res) != 2 {
			err = fmt.Errorf("interval parameter contains unknown parameters")
			return
		}
		if res[1] == "all" {
			keepCount = RetentionGridKeepCountAll
		} else {
			keepCount, err = strconv.Atoi(res[1])
			if err != nil {
				err = fmt.Errorf("cannot parse keep_count value")
				return
			}
		}
	}

	intervals = make([]RetentionInterval, times)
	for i := range intervals {
		intervals[i] = RetentionInterval{
			length:    duration,
			keepCount: keepCount,
		}
	}

	return

}

func parseRetentionGridIntervalsString(s string) (intervals []RetentionInterval, err error) {

	ges := strings.Split(s, "|")
	intervals = make([]RetentionInterval, 0, 7*len(ges))

	for intervalIdx, e := range ges {
		parsed, err := parseRetentionGridIntervalString(e)
		if err != nil {
			return nil, fmt.Errorf("cannot parse interval %d of %d: %s: %s", intervalIdx+1, len(ges), err, strings.TrimSpace(e))
		}
		intervals = append(intervals, parsed...)
	}

	return
}
