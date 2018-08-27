package job

import (
	"github.com/zrepl/zrepl/cmd/config"
	"fmt"
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
	default:
		panic(fmt.Sprintf("implementation error: unknown job type %s", v))
	}

}