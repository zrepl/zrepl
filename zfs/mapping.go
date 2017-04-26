package zfs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

type DatasetMapping interface {
	Map(source DatasetPath) (target DatasetPath, err error)
}

type GlobMapping struct {
	PrefixPath DatasetPath
	TargetRoot DatasetPath
}

var NoMatchError error = errors.New("no match found in mapping")

func (m GlobMapping) Map(source DatasetPath) (target DatasetPath, err error) {

	if len(source) < len(m.PrefixPath) {
		err = NoMatchError
		return
	}

	target = make([]string, 0, len(source)+len(m.TargetRoot))
	target = append(target, m.TargetRoot...)

	for si, sc := range source {
		target = append(target, sc)
		if si < len(m.PrefixPath) {
			if sc != m.PrefixPath[si] {
				err = NoMatchError
				return
			}
			continue
		}
	}

	return
}

type ComboMapping struct {
	Mappings []DatasetMapping
}

func (m ComboMapping) Map(source DatasetPath) (target DatasetPath, err error) {
	for _, sm := range m.Mappings {
		target, err = sm.Map(source)
		if err == nil {
			return target, err
		}
	}
	return nil, NoMatchError
}

type DirectMapping struct {
	Source DatasetPath
	Target DatasetPath
}

func (m DirectMapping) Map(source DatasetPath) (target DatasetPath, err error) {

	if m.Source == nil {
		return m.Target, nil
	}

	if len(m.Source) != len(source) {
		return nil, NoMatchError
	}

	for i, c := range source {
		if c != m.Source[i] {
			return nil, NoMatchError
		}
	}

	return m.Target, nil
}

type ExecMapping struct {
	Name string
	Args []string
}

func NewExecMapping(name string, args ...string) (m *ExecMapping) {
	m = &ExecMapping{
		Name: name,
		Args: args,
	}
	return
}

func (m ExecMapping) Map(source DatasetPath) (target DatasetPath, err error) {

	var stdin io.Writer
	var stdout io.Reader

	cmd := exec.Command(m.Name, m.Args...)

	if stdin, err = cmd.StdinPipe(); err != nil {
		return
	}

	if stdout, err = cmd.StdoutPipe(); err != nil {
		return
	}

	resp := bufio.NewScanner(stdout)

	if err = cmd.Start(); err != nil {
		return
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			fmt.Printf("error: %v\n", err) // TODO
		}
	}()

	if _, err = io.WriteString(stdin, source.ToString()+"\n"); err != nil {
		return
	}

	if !resp.Scan() {
		err = errors.New(fmt.Sprintf("unexpected end of file: %v", resp.Err()))
		return
	}

	t := resp.Text()

	switch {
	case t == "NOMAP":
		return nil, NoMatchError
	}

	target = toDatasetPath(t) // TODO discover garbage?

	return
}
