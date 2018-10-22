package envconst

import (
	"os"
	"sync"
	"time"
)

var cache sync.Map

func Duration(varname string, def time.Duration) time.Duration {
	if v, ok := cache.Load(varname); ok {
		return v.(time.Duration)
	}
	e := os.Getenv(varname)
	if e == "" {
		return def
	}
	d, err := time.ParseDuration(e)
	if err != nil {
		panic(err)
	}
	cache.Store(varname, d)
	return d
}
