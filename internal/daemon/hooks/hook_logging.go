package hooks

import (
	"bufio"
	"bytes"
	"context"
	"sync"

	"github.com/LyingCak3/zrepl/internal/daemon/logging"
	"github.com/LyingCak3/zrepl/internal/logger"
	"github.com/LyingCak3/zrepl/internal/util/envconst"
)

type Logger = logger.Logger

func GetLogger(ctx context.Context) Logger { return getLogger(ctx) }

func getLogger(ctx context.Context) Logger {
	return logging.GetLogger(ctx, logging.SubsysHooks)
}

const MAX_HOOK_LOG_SIZE_DEFAULT int = 1 << 20

type logWriter struct {
	/*
		Mutex prevents:
			concurrent writes to buf, scanner in Write([]byte)
			data race on scanner vs Write([]byte)
				and concurrent write to buf (call to buf.Reset())
				in Close()

		(Also, Close() should generally block until any Write() call completes.)
	*/
	mtx     *sync.Mutex
	buf     bytes.Buffer
	scanner *bufio.Scanner
	logger  Logger
	level   logger.Level
	field   string
}

func NewLogWriter(mtx *sync.Mutex, logger Logger, level logger.Level, field string) *logWriter {
	w := new(logWriter)
	w.mtx = mtx
	w.scanner = bufio.NewScanner(&w.buf)
	w.logger = logger
	w.level = level
	w.field = field
	return w
}

func (w *logWriter) log(line string) {
	w.logger.WithField(w.field, line).Log(w.level, "hook output")
}

func (w *logWriter) logUnreadBytes() error {
	for w.scanner.Scan() {
		w.log(w.scanner.Text())
	}
	if w.buf.Cap() > envconst.Int("ZREPL_MAX_HOOK_LOG_SIZE", MAX_HOOK_LOG_SIZE_DEFAULT) {
		w.buf.Reset()
	}

	return nil
}

func (w *logWriter) Write(in []byte) (int, error) {
	w.mtx.Lock()
	defer w.mtx.Unlock()

	n, bufErr := w.buf.Write(in)
	if bufErr != nil {
		return n, bufErr
	}

	err := w.logUnreadBytes()
	if err != nil {
		return n, err
	}
	// Always reset the scanner for the next Write
	w.scanner = bufio.NewScanner(&w.buf)

	return n, nil
}

func (w *logWriter) Close() (err error) {
	w.mtx.Lock()
	defer w.mtx.Unlock()

	return w.logUnreadBytes()
}
