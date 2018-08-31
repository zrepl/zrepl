package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/nethelpers"
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
	ControlJobEndpointWakeup  string = "/wakeup"
)

func (j *controlJob) Run(ctx context.Context) {

	log := job.GetLogger(ctx)
	defer log.Info("control job finished")

	l, err := nethelpers.ListenUnixPrivate(j.sockaddr)
	if err != nil {
		log.WithError(err).Error("error listening")
		return
	}

	pprofServer := NewPProfServer(ctx)

	mux := http.NewServeMux()
	mux.Handle(ControlJobEndpointPProf,
		requestLogger{log: log, handler: jsonRequestResponder{func(decoder jsonDecoder) (interface{}, error) {
			var msg PprofServerControlMsg
			err := decoder(&msg)
			if err != nil {
				return nil, errors.Errorf("decode failed")
			}
			pprofServer.Control(msg)
			return struct{}{}, nil
		}}})

	mux.Handle(ControlJobEndpointVersion,
		requestLogger{log: log, handler: jsonResponder{func() (interface{}, error) {
			return version.NewZreplVersionInformation(), nil
		}}})

	mux.Handle(ControlJobEndpointStatus,
		requestLogger{log: log, handler: jsonResponder{func() (interface{}, error) {
			s := j.jobs.status()
			return s, nil
		}}})

	mux.Handle(ControlJobEndpointWakeup,
		requestLogger{log: log, handler: jsonRequestResponder{func(decoder jsonDecoder) (interface{}, error) {
			type reqT struct {
				Name string
			}
			var req reqT
			if decoder(&req) != nil {
				return nil, errors.Errorf("decode failed")
			}

			err := j.jobs.wakeup(req.Name)

			return struct{}{}, err
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

type jsonDecoder = func(interface{}) error

type jsonRequestResponder struct {
	producer func(decoder jsonDecoder) (interface{}, error)
}

func (j jsonRequestResponder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var decodeError error
	decoder := func(i interface{}) error {
		err := json.NewDecoder(r.Body).Decode(&i)
		decodeError = err
		return err
	}
	res, producerErr := j.producer(decoder)

	//If we had a decode error ignore output of producer and return error
	if decodeError != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, decodeError.Error())
		return
	}
	if producerErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, producerErr.Error())
		return
	}

	var buf bytes.Buffer
	encodeErr := json.NewEncoder(&buf).Encode(res)
	if encodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, encodeErr.Error())
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
	log.Debug("start")
	if l.handlerFunc != nil {
		l.handlerFunc(w, r)
	} else if l.handler != nil {
		l.handler.ServeHTTP(w, r)
	} else {
		log.Error("no handler or handlerFunc configured")
	}
	log.Debug("finish")
}
