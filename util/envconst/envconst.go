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

func Int(varname string, def int) int {
	if v, ok := cache.Load(varname); ok {
		return v.(int)
	}
	e := os.Getenv(varname)
	if e == "" {
		return def
	}
	d64, err := strconv.ParseInt(e, 10, strconv.IntSize)
	if err != nil {
		panic(err)
	}
	d := int(d64)
	cache.Store(varname, d)
	return d
}

func Int64(varname string, def int64) int64 {
	if v, ok := cache.Load(varname); ok {
		return v.(int64)
	}
	e := os.Getenv(varname)
	if e == "" {
		return def
	}
	d, err := strconv.ParseInt(e, 10, 64)
	if err != nil {
		panic(err)
	}
	cache.Store(varname, d)
	return d
}

func Bool(varname string, def bool) bool {
	if v, ok := cache.Load(varname); ok {
		return v.(bool)
	}
	e := os.Getenv(varname)
	if e == "" {
		return def
	}
	d, err := strconv.ParseBool(e)
	if err != nil {
		panic(err)
	}
	cache.Store(varname, d)
	return d
}

func String(varname string, def string) string {
	if v, ok := cache.Load(varname); ok {
		return v.(string)
	}
	e := os.Getenv(varname)
	if e == "" {
		return def
	}
	cache.Store(varname, e)
	return e
}

func Var(varname string, def flag.Value) interface{} {

	// use def's type to instantiate a new object of that same type
	// and call flag.Value.Set() on it
	defType := reflect.TypeOf(def)
	if defType.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("envconst var must be a pointer, got %T", def))
	}
	defElemType := defType.Elem()

	if v, ok := cache.Load(varname); ok {
		return v.(string)
	}
	e := os.Getenv(varname)
	if e == "" {
		return def
	}

	newInstance := reflect.New(defElemType)
	if err := newInstance.Interface().(flag.Value).Set(e); err != nil {
		panic(err)
	}

	res := newInstance.Interface()
	cache.Store(varname, res)
	return res
}
