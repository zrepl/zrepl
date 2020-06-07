package envconst

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"sync"
	"time"
)

var cache sync.Map

func Reset() {
	cache.Range(func(key, _ interface{}) bool {
		cache.Delete(key)
		return true
	})
}

func Duration(varname string, def time.Duration) (d time.Duration) {
	var err error
	if v, ok := cache.Load(varname); ok {
		return v.(time.Duration)
	}
	e := os.Getenv(varname)
	if e == "" {
		d = def
	} else {
		d, err = time.ParseDuration(e)
		if err != nil {
			panic(err)
		}
	}
	cache.Store(varname, d)
	return d
}

func Int(varname string, def int) (d int) {
	if v, ok := cache.Load(varname); ok {
		return v.(int)
	}
	e := os.Getenv(varname)
	if e == "" {
		d = def
	} else {
		d64, err := strconv.ParseInt(e, 10, strconv.IntSize)
		if err != nil {
			panic(err)
		}
		d = int(d64)
	}
	cache.Store(varname, d)
	return d
}

func Int64(varname string, def int64) (d int64) {
	var err error
	if v, ok := cache.Load(varname); ok {
		return v.(int64)
	}
	e := os.Getenv(varname)
	if e == "" {
		d = def
	} else {
		d, err = strconv.ParseInt(e, 10, 64)
		if err != nil {
			panic(err)
		}
	}
	cache.Store(varname, d)
	return d
}

func Bool(varname string, def bool) (d bool) {
	var err error
	if v, ok := cache.Load(varname); ok {
		return v.(bool)
	}
	e := os.Getenv(varname)
	if e == "" {
		d = def
	} else {
		d, err = strconv.ParseBool(e)
		if err != nil {
			panic(err)
		}
	}
	cache.Store(varname, d)
	return d
}

func String(varname string, def string) (d string) {
	if v, ok := cache.Load(varname); ok {
		return v.(string)
	}
	e := os.Getenv(varname)
	if e == "" {
		d = def
	} else {
		d = e
	}
	cache.Store(varname, d)
	return d
}

func Var(varname string, def flag.Value) (d interface{}) {

	// use def's type to instantiate a new object of that same type
	// and call flag.Value.Set() on it
	defType := reflect.TypeOf(def)
	if defType.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("envconst var must be a pointer, got %T", def))
	}
	defElemType := defType.Elem()

	if v, ok := cache.Load(varname); ok {
		return v
	}

	e := os.Getenv(varname)
	if e == "" {
		d = def
	} else {
		newInstance := reflect.New(defElemType)
		if err := newInstance.Interface().(flag.Value).Set(e); err != nil {
			panic(err)
		}
		d = newInstance.Interface()
	}

	cache.Store(varname, d)
	return d
}

type Report struct {
	Entries []EntryReport
}

type EntryReport struct {
	Var         string
	Value       string
	ValueGoType string
}

func GetReport() *Report {
	var r Report
	cache.Range(func(key, value interface{}) bool {
		r.Entries = append(r.Entries, EntryReport{
			Var:         key.(string),
			Value:       fmt.Sprintf("%v", value),
			ValueGoType: fmt.Sprintf("%T", value),
		})
		return true
	})
	return &r
}
