package trace

// The functions in this file are concerned with the generation
// of trace files based on the information from WithTask and WithSpan.
//
// The emitted trace files are open-ended array of JSON objects
// that follow the Chrome trace file format:
//   https://docs.google.com/document/d/1CvAClvFfyA5R-PhYUmn5OOQtYMH4h6I0nSsKchNAySU/preview
//
// The emitted JSON can be loaded into Chrome's chrome://tracing view.
//
// The trace file can be written to a file whose path is specified in an env file,
// and be written to web sockets established on ChrometraceHttpHandler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"golang.org/x/net/websocket"

	"github.com/zrepl/zrepl/internal/util/envconst"
)

var chrometracePID string

func init() {
	var err error
	chrometracePID, err = os.Hostname()
	if err != nil {
		panic(err)
	}
	chrometracePID = fmt.Sprintf("%q", chrometracePID)
}

type chrometraceEvent struct {
	Cat                       string   `json:"cat,omitempty"`
	Name                      string   `json:"name"`
	Stack                     []string `json:"stack,omitempty"`
	Phase                     string   `json:"ph"`
	TimestampUnixMicroseconds int64    `json:"ts"`
	DurationMicroseconds      int64    `json:"dur,omitempty"`
	Pid                       string   `json:"pid"`
	Tid                       string   `json:"tid"`
	Id                        string   `json:"id,omitempty"`
}

func chrometraceBeginSpan(s *traceNode) {
	taskName := s.TaskName()
	chrometraceWrite(chrometraceEvent{
		Name:                      s.annotation,
		Phase:                     "B",
		TimestampUnixMicroseconds: s.startedAt.UnixNano() / 1000,
		Pid:                       chrometracePID,
		Tid:                       taskName,
	})
}

func chrometraceEndSpan(s *traceNode) {
	taskName := s.TaskName()
	chrometraceWrite(chrometraceEvent{
		Name:                      s.annotation,
		Phase:                     "E",
		TimestampUnixMicroseconds: s.endedAt.UnixNano() / 1000,
		Pid:                       chrometracePID,
		Tid:                       taskName,
	})
}

var chrometraceFlowId uint64

func chrometraceBeginTask(s *traceNode) {
	chrometraceBeginSpan(s)

	if s.parentTask == nil {
		return
	}
	// beginning of a task that has a parent
	// => use flow events to link parent and child

	flowId := atomic.AddUint64(&chrometraceFlowId, 1)
	flowIdStr := fmt.Sprintf("%x", flowId)

	parentTask := s.parentTask.TaskName()
	chrometraceWrite(chrometraceEvent{
		Cat:                       "task", // seems to be necessary, otherwise the GUI shows some `indexOf` JS error
		Name:                      "child-task",
		Phase:                     "s",
		TimestampUnixMicroseconds: s.startedAt.UnixNano() / 1000, // yes, the child's timestamp (=> from-point of the flow line is at right x-position of parent's bar)
		Pid:                       chrometracePID,
		Tid:                       parentTask,
		Id:                        flowIdStr,
	})

	childTask := s.TaskName()
	if parentTask == childTask {
		panic(parentTask)
	}
	chrometraceWrite(chrometraceEvent{
		Cat:                       "task", // seems to be necessary, otherwise the GUI shows some `indexOf` JS error
		Name:                      "child-task",
		Phase:                     "f",
		TimestampUnixMicroseconds: s.startedAt.UnixNano() / 1000,
		Pid:                       chrometracePID,
		Tid:                       childTask,
		Id:                        flowIdStr,
	})
}

func chrometraceEndTask(s *traceNode) {
	chrometraceEndSpan(s)
}

type chrometraceConsumerRegistration struct {
	w io.Writer
	// errored must have capacity 1, the writer thread will send to it non-blocking, then close it
	errored chan error
}

var chrometraceConsumers struct {
	register  chan chrometraceConsumerRegistration
	consumers map[chrometraceConsumerRegistration]bool
	write     chan []byte
}

func init() {
	chrometraceConsumers.register = make(chan chrometraceConsumerRegistration)
	chrometraceConsumers.consumers = make(map[chrometraceConsumerRegistration]bool)
	chrometraceConsumers.write = make(chan []byte)
	go func() {
		kickConsumer := func(c chrometraceConsumerRegistration, err error) {
			debug("chrometrace kicking consumer %#v after error %v", c, err)
			select {
			case c.errored <- err:
			default:
			}
			close(c.errored)
			delete(chrometraceConsumers.consumers, c)
		}
		for {
			select {
			case reg := <-chrometraceConsumers.register:
				debug("registered chrometrace consumer %#v", reg)
				chrometraceConsumers.consumers[reg] = true
				n, err := reg.w.Write([]byte("[\n"))
				if err != nil {
					kickConsumer(reg, err)
				} else if n != 2 {
					kickConsumer(reg, fmt.Errorf("short write: %v", n))
				}
				// successfully registered

			case buf := <-chrometraceConsumers.write:
				debug("chrometrace write request: %s", string(buf))
				var r bytes.Reader
				for c := range chrometraceConsumers.consumers {
					r.Reset(buf)
					n, err := io.Copy(c.w, &r)
					debug("chrometrace wrote n=%v bytes to consumer %#v", n, c)
					if err != nil {
						kickConsumer(c, err)
					}
				}
			}
		}
	}()
}

func chrometraceWrite(i interface{}) {
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(i)
	if err != nil {
		panic(err)
	}
	buf.WriteString(",")
	chrometraceConsumers.write <- buf.Bytes()
}

func ChrometraceClientWebsocketHandler(conn *websocket.Conn) {
	defer conn.Close()

	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := bufio.NewReader(conn)
		_, _, _ = r.ReadLine() // ignore errors
		conn.Close()
	}()

	errored := make(chan error, 1)
	chrometraceConsumers.register <- chrometraceConsumerRegistration{
		w:       conn,
		errored: errored,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-errored
		conn.Close()
	}()
}

var chrometraceFileConsumerPath = envconst.String("ZREPL_ACTIVITY_TRACE", "")

func init() {
	if chrometraceFileConsumerPath != "" {
		var err error
		f, err := os.Create(chrometraceFileConsumerPath)
		if err != nil {
			panic(err)
		}
		errored := make(chan error, 1)
		chrometraceConsumers.register <- chrometraceConsumerRegistration{
			w:       f,
			errored: errored,
		}
		go func() {
			<-errored
			f.Close()
		}()
	}
}
