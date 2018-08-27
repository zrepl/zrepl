package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/cmd/daemon/job"
	"github.com/zrepl/zrepl/cmd/helpers"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/version"
	"io"
	"net"
	"net/http"
)

type controlJob struct {
	sockaddr *net.UnixAddr
	jobs     *jobs
}

func newControlJob(sockpath string, jobs *jobs) (j *controlJob, err error) {
	j = &controlJob{jobs: jobs}

	j.sockaddr, err = net.ResolveUnixAddr("unix", sockpath)
	if err != nil {
		err = errors.Wrap(err, "cannot resolve unix address")
		return
	}

	return
}

func (j *controlJob) Name() string { return jobNameControl }

func (j *controlJob) Status() interface{} { return nil }

const (
	ControlJobEndpointPProf   string = "/debug/pprof"
	ControlJobEndpointVersion string = "/version"
	ControlJobEndpointStatus  string = "/status"
)

func (j *controlJob) Run(ctx context.Context) {

	log := job.GetLogger(ctx)
	defer log.Info("control job finished")

	l, err := helpers.ListenUnixPrivate(j.sockaddr)
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
			return version.NewZreplVersionInformation(), nil
		}}})
	mux.Handle(ControlJobEndpointStatus,
		requestLogger{log: log, handler: jsonResponder{func() (interface{}, error) {
			s := j.jobs.status()
			return s, nil
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
	log         logger.Logger
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
