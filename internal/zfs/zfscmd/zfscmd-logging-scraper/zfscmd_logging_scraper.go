package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-logfmt/logfmt"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/internal/daemon/logging"
)

type RuntimeLine struct {
	LogTime                      time.Time
	Cmd                          string
	TotalTime, Usertime, Systime time.Duration
	Error                        string
}

var humanFormatterLineRE = regexp.MustCompile(`^(\[[^\]]+\]){2}\[zfs.cmd\]\[[^\]]+\]:\s+command\s+exited\s+with\s+(success|error)\s+(.+)`)

func parseSecs(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s + "s")
	if err != nil {
		return 0, errors.Wrapf(err, "parse duration %q", s)
	}
	return d, nil
}

func parseHumanFormatterNodate(line string) (l RuntimeLine, err error) {
	m := humanFormatterLineRE.FindStringSubmatch(line)
	if m == nil {
		return l, errors.New("human formatter regex does not match")
	}

	d := logfmt.NewDecoder(strings.NewReader(m[3]))
	for d.ScanRecord() {
		for d.ScanKeyval() {
			k := string(d.Key())
			v := string(d.Value())
			switch k {
			case "cmd":
				l.Cmd = v
			case "total_time_s":
				l.TotalTime, err = parseSecs(v)
			case "usertime_s":
				l.Usertime, err = parseSecs(v)
			case "systemtime_s":
				l.Systime, err = parseSecs(v)
			case "err":
				l.Error = v
			case "invocation":
				continue // pass
			default:
				return l, errors.Errorf("unknown key %q", k)
			}
			if err != nil {
				return l, err
			}
		}
	}
	if d.Err() != nil {
		return l, errors.Wrap(d.Err(), "decode key value pairs")
	}
	return l, nil
}

func parseLogLine(line string) (l RuntimeLine, err error) {
	m := dateRegex.FindStringSubmatch(line)
	if len(m) != 3 {
		return l, errors.Errorf("invalid date regex match %v", m)
	}
	dateTrimmed := strings.TrimSpace(m[1])
	date, err := time.Parse(dateFormat, dateTrimmed)
	if err != nil {
		panic(fmt.Sprintf("cannot parse date %q: %s", dateTrimmed, err))
	}
	logLine := m[2]

	l, err = parseHumanFormatterNodate(strings.TrimSpace(logLine))
	l.LogTime = date
	return l, err
}

var verbose bool
var dateRegexArg string
var dateRegex = regexp.MustCompile(`^([^\[]+)(.*)`)
var dateFormat = logging.HumanFormatterDateFormat

func main() {

	pflag.StringVarP(&dateRegexArg, "dateRE", "d", "", "date regex")
	pflag.StringVar(&dateFormat, "dateFormat", logging.HumanFormatterDateFormat, "go date format")
	pflag.BoolVarP(&verbose, "verbose", "v", false, "verbose")
	pflag.Parse()

	if dateRegexArg != "" {
		dateRegex = regexp.MustCompile(dateRegexArg)
	}

	input := bufio.NewScanner(os.Stdin)
	input.Split(bufio.ScanLines)

	enc := json.NewEncoder(os.Stdout)
	for input.Scan() {

		l, err := parseLogLine(input.Text())
		if err != nil && verbose {
			fmt.Fprintf(os.Stderr, "ignoring line after error %v\n", err)
			fmt.Fprintf(os.Stderr, "offending line was: %s\n", input.Text())
		}
		if err == nil {
			if err := enc.Encode(l); err != nil {
				panic(err)
			}
		}
	}

	if input.Err() != nil {
		panic(input.Err())
	}

}
