package cmd

import (
	"context"
	"github.com/pkg/errors"
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
	defer log.Info("control job finished")

	l, err := ListenUnixPrivate(j.sockaddr)
	if err != nil {
		log.WithError(err).Error("error listening")
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
			log.WithError(err).Info("contex done")
			server.Shutdown(context.Background())
			break outer
		case err = <-served:
			if err != nil {
				log.WithError(err).Error("error serving")
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
	log := l.log.WithField("method", r.Method).WithField("url", r.URL)
	log.Info("start")
	l.handlerFunc(w, r)
	log.Info("finish")
}
