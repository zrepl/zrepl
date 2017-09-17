package cmd

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/util"
	"net"
	"net/http"
	"net/http/pprof"
)

type ControlJob struct {
	Name     string
	sockaddr *net.UnixAddr
}

func NewControlJob(name, sockpath string) (j *ControlJob, err error) {
	j = &ControlJob{Name: name}

	j.sockaddr, err = net.ResolveUnixAddr("unix", sockpath)
	if err != nil {
		err = errors.Wrap(err, "cannot resolve unix address")
		return
	}

	return
}

func (j *ControlJob) JobName() string {
	return j.Name
}

const (
	ControlJobEndpointProfile string = "/debug/pprof/profile"
)

func (j *ControlJob) JobStart(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)
	defer log.Printf("control job finished")

	l, err := ListenUnixPrivate(j.sockaddr)
	if err != nil {
		log.Printf("error listening: %s", err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle(ControlJobEndpointProfile, requestLogger{log, pprof.Profile})
	server := http.Server{Handler: mux}

outer:
	for {

		served := make(chan error)
		go func() {
			served <- server.Serve(l)
			close(served)
		}()

		select {
		case <-ctx.Done():
			log.Printf("context: %s", ctx.Err())
			server.Shutdown(context.Background())
			break outer
		case err = <-served:
			if err != nil {
				log.Printf("error serving: %s", err)
				break outer
			}
		}

	}

}

type requestLogger struct {
	log         Logger
	handlerFunc func(w http.ResponseWriter, r *http.Request)
}

func (l requestLogger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := util.NewPrefixLogger(l.log, fmt.Sprintf("%s %s", r.Method, r.URL))
	log.Printf("start")
	l.handlerFunc(w, r)
	log.Printf("finish")
}
