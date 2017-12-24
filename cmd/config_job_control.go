package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	"io"
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
	ControlJobEndpointVersion string = "/version"
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
	mux.Handle(ControlJobEndpointProfile, requestLogger{log: log, handlerFunc: pprof.Profile})
	mux.Handle(ControlJobEndpointVersion,
		requestLogger{log: log, handler: jsonResponder{func() (interface{}, error) {
			return NewZreplVersionInformation(), nil
		}}})
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
			log.WithError(ctx.Err()).Info("context done")
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

type jsonResponder struct {
	producer func() (interface{}, error)
}

func (j jsonResponder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	res, err := j.producer()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, err.Error())
		return
	}
	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(res)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, err.Error())
	} else {
		io.Copy(w, &buf)
	}
}

type requestLogger struct {
	log         Logger
	handler     http.Handler
	handlerFunc http.HandlerFunc
}

func (l requestLogger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := l.log.WithField("method", r.Method).WithField("url", r.URL)
	log.Info("start")
	if l.handlerFunc != nil {
		l.handlerFunc(w, r)
	} else if l.handler != nil {
		l.handler.ServeHTTP(w, r)
	} else {
		log.Error("no handler or handlerFunc configured")
	}
	log.Info("finish")
}
