package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	"io"
	"net"
	"net/http"
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

func (j *ControlJob) JobType() JobType { return JobTypeControl }

func (j *ControlJob) JobStatus(ctx context.Context) (*JobStatus, error) {
	return &JobStatus{Tasks: nil}, nil
}

const (
	ControlJobEndpointPProf   string = "/debug/pprof"
	ControlJobEndpointVersion string = "/version"
	ControlJobEndpointStatus  string = "/status"
)

func (j *ControlJob) JobStart(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)
	defer log.Info("control job finished")

	daemon := ctx.Value(contextKeyDaemon).(*Daemon)

	l, err := ListenUnixPrivate(j.sockaddr)
	if err != nil {
		log.WithError(err).Error("error listening")
		return
	}

	pprofServer := NewPProfServer(ctx)

	mux := http.NewServeMux()
	mux.Handle(ControlJobEndpointPProf, requestLogger{log: log, handlerFunc: func(w http.ResponseWriter, r *http.Request) {
		var msg PprofServerControlMsg
		err := json.NewDecoder(r.Body).Decode(&msg)
		if err != nil {
			log.WithError(err).Error("bad pprof request from client")
			w.WriteHeader(http.StatusBadRequest)
		}
		pprofServer.Control(msg)
		w.WriteHeader(200)
	}})
	mux.Handle(ControlJobEndpointVersion,
		requestLogger{log: log, handler: jsonResponder{func() (interface{}, error) {
			return NewZreplVersionInformation(), nil
		}}})
	mux.Handle(ControlJobEndpointStatus,
		requestLogger{log: log, handler: jsonResponder{func() (interface{}, error) {
			return daemon.Status(), nil
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
