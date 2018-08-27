package job

import (
	"fmt"
	"github.com/zrepl/zrepl/config"
)

func JobsFromConfig(c config.Config) ([]Job, error) {
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

func buildJob(c config.Global, in config.JobEnum) (j Job, err error) {

	switch v := in.Ret.(type) {
	case *config.SinkJob:
		return SinkFromConfig(c, v)
	case *config.PushJob:
		return PushFromConfig(c, v)
	default:
		panic(fmt.Sprintf("implementation error: unknown job type %T", v))
	}

}
