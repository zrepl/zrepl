package job

import (
	"fmt"
	"github.com/zrepl/zrepl/config"
	"github.com/pkg/errors"
)

func JobsFromConfig(c *config.Config) ([]Job, error) {
	js := make([]Job, len(c.Jobs))
	for i := range c.Jobs {
		j, err := buildJob(c.Global, c.Jobs[i])
		if err != nil {
			return nil, err
		}
		js[i] = j
	}
	return js, nil
}

func buildJob(c *config.Global, in config.JobEnum) (j Job, err error) {
	switch v := in.Ret.(type) {
	case *config.SinkJob:
		j, err = SinkFromConfig(c, v)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot build job %q", v.Name)
		}
	case *config.PushJob:
		j, err = PushFromConfig(c, v)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot build job %q", v.Name)
		}
	default:
		panic(fmt.Sprintf("implementation error: unknown job type %T", v))
	}
	return j, err
}
