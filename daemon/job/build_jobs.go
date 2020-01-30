package job

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
)

func JobsFromConfig(c *config.Config) ([]Job, error) {
	js := make([]Job, len(c.Jobs))
	for i := range c.Jobs {
		j, err := buildJob(c.Global, c.Jobs[i])
		if err != nil {
			return nil, err
		}
		if j == nil || j.Name() == "" {
			panic(fmt.Sprintf("implementation error: job builder returned nil job type %T", c.Jobs[i].Ret))
		}
		js[i] = j
	}

	// receiving-side root filesystems must not overlap
	{
		rfss := make([]string, 0, len(js))
		for _, j := range js {
			jrfs, ok := j.OwnedDatasetSubtreeRoot()
			if !ok {
				continue
			}
			rfss = append(rfss, jrfs.ToString())
		}
		if err := validateReceivingSidesDoNotOverlap(rfss); err != nil {
			return nil, err
		}
	}

	return js, nil
}

func buildJob(c *config.Global, in config.JobEnum) (j Job, err error) {
	cannotBuildJob := func(e error, name string) (Job, error) {
		return nil, errors.Wrapf(e, "cannot build job %q", name)
	}
	// FIXME prettify this
	switch v := in.Ret.(type) {
	case *config.SinkJob:
		m, err := modeSinkFromConfig(c, v)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
		j, err = passiveSideFromConfig(c, &v.PassiveJob, m)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
	case *config.SourceJob:
		m, err := modeSourceFromConfig(c, v)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
		j, err = passiveSideFromConfig(c, &v.PassiveJob, m)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
	case *config.SnapJob:
		j, err = snapJobFromConfig(c, v)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
	case *config.PushJob:
		m, err := modePushFromConfig(c, v)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
		j, err = activeSide(c, &v.ActiveJob, m)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
	case *config.PullJob:
		m, err := modePullFromConfig(c, v)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
		j, err = activeSide(c, &v.ActiveJob, m)
		if err != nil {
			return cannotBuildJob(err, v.Name)
		}
	default:
		panic(fmt.Sprintf("implementation error: unknown job type %T", v))
	}
	return j, nil

}

func validateReceivingSidesDoNotOverlap(receivingRootFSs []string) error {
	if len(receivingRootFSs) == 0 {
		return nil
	}
	rfss := make([]string, len(receivingRootFSs))
	copy(rfss, receivingRootFSs)
	sort.Slice(rfss, func(i, j int) bool {
		return strings.Compare(rfss[i], rfss[j]) == -1
	})
	// add tailing slash because of hierarchy-simulation
	// rootfs/ is not root of rootfs2/
	for i := 0; i < len(rfss); i++ {
		rfss[i] += "/"
	}
	// idea:
	//   no path in rfss must be prefix of another
	//
	// rfss is now lexicographically sorted, which means that
	// if i is prefix of j, i < j (in lexicographical order)
	// thus,
	// if any i is prefix of i+n (n >= 1), there is overlap
	for i := 0; i < len(rfss)-1; i++ {
		if strings.HasPrefix(rfss[i+1], rfss[i]) {
			return fmt.Errorf("receiving jobs with overlapping root filesystems are forbidden")
		}
	}
	return nil
}
