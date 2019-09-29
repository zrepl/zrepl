package platformtest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

type Execer interface {
	RunExpectSuccessNoOutput(ctx context.Context, cmd string, args ...string) error
	RunExpectFailureNoOutput(ctx context.Context, cmd string, args ...string) error
}

type Stmt interface {
	Run(context context.Context, e Execer) error
}

type Op string

const (
	AssertExists    Op = "!E"
	AssertNotExists Op = "!N"
	Add             Op = "+"
	Del             Op = "-"
	RunCmd          Op = "R"
	DestroyRoot     Op = "DESTROYROOT"
	CreateRoot      Op = "CREATEROOT"
)

type DestroyRootOp struct {
	Path string
}

func (o *DestroyRootOp) Run(ctx context.Context, e Execer) error {
	// early-exit if it doesn't exist
	if err := e.RunExpectSuccessNoOutput(ctx, "zfs", "get", "-H", "name", o.Path); err != nil {
		getLog(ctx).WithField("root_ds", o.Path).Info("assume root ds doesn't exist")
		return nil
	}
	return e.RunExpectSuccessNoOutput(ctx, "zfs", "destroy", "-r", o.Path)
}

type FSOp struct {
	Op   Op
	Path string
}

func (o *FSOp) Run(ctx context.Context, e Execer) error {
	switch o.Op {
	case AssertExists:
		return e.RunExpectSuccessNoOutput(ctx, "zfs", "get", "-H", "name", o.Path)
	case AssertNotExists:
		return e.RunExpectFailureNoOutput(ctx, "zfs", "get", "-H", "name", o.Path)
	case Add:
		return e.RunExpectSuccessNoOutput(ctx, "zfs", "create", o.Path)
	case Del:
		return e.RunExpectSuccessNoOutput(ctx, "zfs", "destroy", o.Path)
	default:
		panic(o.Op)
	}
}

type SnapOp struct {
	Op   Op
	Path string
}

func (o *SnapOp) Run(ctx context.Context, e Execer) error {
	switch o.Op {
	case AssertExists:
		return e.RunExpectSuccessNoOutput(ctx, "zfs", "get", "-H", "name", o.Path)
	case AssertNotExists:
		return e.RunExpectFailureNoOutput(ctx, "zfs", "get", "-H", "name", o.Path)
	case Add:
		return e.RunExpectSuccessNoOutput(ctx, "zfs", "snapshot", o.Path)
	case Del:
		return e.RunExpectSuccessNoOutput(ctx, "zfs", "destroy", o.Path)
	default:
		panic(o.Op)
	}
}

type RunOp struct {
	RootDS string
	Script string
}

func (o *RunOp) Run(ctx context.Context, e Execer) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/env", "bash", "-c", o.Script)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("ROOTDS=%s", o.RootDS))
	log := getLog(ctx).WithField("script", o.Script)
	log.Info("start script")
	defer log.Info("script done")
	output, err := cmd.CombinedOutput()
	if _, ok := err.(*exec.ExitError); err != nil && !ok {
		panic(err)
	}
	log.Printf("script output:\n%s", output)
	return err
}

type LineError struct {
	Line string
	What string
}

func (e LineError) Error() string {
	return fmt.Sprintf("%q: %s", e.Line, e.What)
}

type RunKind int

const (
	PanicErr RunKind = 1 << iota
	RunAll
)

func Run(ctx context.Context, rk RunKind, rootds string, stmtsStr string) {
	stmt, err := parseSequence(rootds, stmtsStr)
	if err != nil {
		panic(err)
	}
	execer := NewEx(getLog(ctx))
	for _, s := range stmt {
		err := s.Run(ctx, execer)
		if err == nil {
			continue
		}
		if rk == PanicErr {
			panic(err)
		} else if rk == RunAll {
			continue
		} else {
			panic(rk)
		}
	}
}

func isNoSpace(r rune) bool {
	return !unicode.IsSpace(r)
}

func splitQuotedWords(data []byte, atEOF bool) (advance int, token []byte, err error) {
	begin := bytes.IndexFunc(data, isNoSpace)
	if begin == -1 {
		return len(data), nil, nil
	}
	if data[begin] == '"' {
		end := begin + 1
		for end < len(data) {
			endCandidate := bytes.Index(data[end:], []byte(`"`))
			if endCandidate == -1 {
				return 0, nil, nil
			}
			end += endCandidate
			if data[end-1] != '\\' {
				// unescaped quote, end of this string
				// remove backslash-escapes
				withBackslash := data[begin+1 : end]
				withoutBaskslash := bytes.Replace(withBackslash, []byte("\\\""), []byte("\""), -1)
				return end + 1, withoutBaskslash, nil
			} else {
				// continue to next quote
				end += 1
			}
		}
	} else {
		endOffset := bytes.IndexFunc(data[begin:], unicode.IsSpace)
		var end int
		if endOffset == -1 {
			if !atEOF {
				return 0, nil, nil
			} else {
				end = len(data)
			}
		} else {
			end = begin + endOffset
		}
		return end, data[begin:end], nil
	}
	return 0, nil, fmt.Errorf("unexpected")
}

func parseSequence(rootds, stmtsStr string) (stmts []Stmt, err error) {
	scan := bufio.NewScanner(strings.NewReader(stmtsStr))
nextLine:
	for scan.Scan() {
		if len(bytes.TrimSpace(scan.Bytes())) == 0 {
			continue
		}
		comps := bufio.NewScanner(bytes.NewReader(scan.Bytes()))
		comps.Split(splitQuotedWords)

		expectMoreTokens := func() error {
			if !comps.Scan() {
				return &LineError{scan.Text(), "unexpected EOL"}
			}
			return nil
		}

		// Op
		if err := expectMoreTokens(); err != nil {
			return nil, err
		}
		var op Op
		switch comps.Text() {

		case string(RunCmd):
			script := strings.TrimPrefix(strings.TrimSpace(scan.Text()), string(RunCmd))
			stmts = append(stmts, &RunOp{RootDS: rootds, Script: script})
			continue nextLine

		case string(DestroyRoot):
			if comps.Scan() {
				return nil, &LineError{scan.Text(), fmt.Sprintf("unexpected tokens at EOL")}
			}
			stmts = append(stmts, &DestroyRootOp{rootds})
			continue nextLine

		case string(CreateRoot):
			if comps.Scan() {
				return nil, &LineError{scan.Text(), fmt.Sprintf("unexpected tokens at EOL")}
			}
			stmts = append(stmts, &FSOp{Op: Add, Path: rootds})
			continue nextLine

		case string(Add):
			op = Add
		case string(Del):
			op = Del
		case string(AssertExists):
			op = AssertExists
		case string(AssertNotExists):
			op = AssertNotExists
		default:
			return nil, &LineError{scan.Text(), fmt.Sprintf("invalid op %q", comps.Text())}
		}

		// FS / SNAP
		if err := expectMoreTokens(); err != nil {
			return nil, err
		}
		if strings.ContainsAny(comps.Text(), "@") {
			stmts = append(stmts, &SnapOp{Op: op, Path: fmt.Sprintf("%s/%s", rootds, comps.Text())})
		} else {
			stmts = append(stmts, &FSOp{Op: op, Path: fmt.Sprintf("%s/%s", rootds, comps.Text())})
		}

		if comps.Scan() {
			return nil, &LineError{scan.Text(), fmt.Sprintf("unexpected tokens at EOL")}
		}
	}
	return stmts, nil
}
