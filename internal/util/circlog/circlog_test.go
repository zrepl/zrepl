package circlog_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/internal/util/circlog"
)

func TestCircularLog(t *testing.T) {
	var maxCircularLogSize int = 1 << 20

	writeFixedSize := func(w io.Writer, size int) (int, error) {
		pattern := []byte{0xDE, 0xAD, 0xBE, 0xEF}
		writeBytes := bytes.Repeat(pattern, size/4)
		if len(writeBytes) < size {
			writeBytes = append(writeBytes, pattern[:size-len(writeBytes)]...)
		}
		n, err := w.Write(writeBytes)
		if err != nil {
			return n, err
		}
		return n, nil
	}

	t.Run("negative-size-error", func(t *testing.T) {
		_, err := circlog.NewCircularLog(-1)
		require.EqualError(t, err, "max must be positive")
	})

	t.Run("no-resize", func(t *testing.T) {
		log, err := circlog.NewCircularLog(maxCircularLogSize)
		require.NoError(t, err)

		initSize := log.Size()
		writeSize := circlog.CIRCULARLOG_INIT_SIZE - 1
		_, err = writeFixedSize(log, writeSize)
		require.NoError(t, err)
		finalSize := log.Size()
		require.Equal(t, initSize, finalSize)
		require.Equal(t, writeSize, log.Len())
	})
	t.Run("one-resize", func(t *testing.T) {
		log, err := circlog.NewCircularLog(maxCircularLogSize)
		require.NoError(t, err)

		initSize := log.Size()
		writeSize := circlog.CIRCULARLOG_INIT_SIZE + 1
		_, err = writeFixedSize(log, writeSize)
		require.NoError(t, err)
		finalSize := log.Size()
		require.Greater(t, finalSize, initSize)
		require.LessOrEqual(t, finalSize, maxCircularLogSize)
		require.Equal(t, writeSize, log.Len())
	})
	t.Run("reset", func(t *testing.T) {
		log, err := circlog.NewCircularLog(maxCircularLogSize)
		require.NoError(t, err)

		initSize := log.Size()
		_, err = writeFixedSize(log, maxCircularLogSize)
		require.NoError(t, err)
		log.Reset()
		finalSize := log.Size()
		require.Equal(t, initSize, finalSize)
	})
	t.Run("wrap-exactly-maximum", func(t *testing.T) {
		log, err := circlog.NewCircularLog(maxCircularLogSize)
		require.NoError(t, err)

		initSize := log.Size()
		writeSize := maxCircularLogSize / 4
		for written := 0; written < maxCircularLogSize; {
			n, err := writeFixedSize(log, writeSize)
			written += n
			require.NoError(t, err)
		}
		finalSize := log.Size()
		require.Greater(t, finalSize, initSize)
		require.Equal(t, finalSize, maxCircularLogSize)
		require.Equal(t, finalSize, log.Len())
	})
	t.Run("wrap-partial", func(t *testing.T) {
		log, err := circlog.NewCircularLog(maxCircularLogSize)
		require.NoError(t, err)

		initSize := log.Size()

		startSentinel := []byte{0x00, 0x00, 0x00, 0x00}
		_, err = log.Write(startSentinel)
		require.NoError(t, err)

		nWritesToFill := 4
		writeSize := maxCircularLogSize / nWritesToFill
		for i := 0; i < (nWritesToFill + 1); i++ {
			_, err := writeFixedSize(log, writeSize)
			require.NoError(t, err)
		}

		endSentinel := []byte{0xFF, 0xFF, 0xFF, 0xFF}
		_, err = log.Write(endSentinel)
		require.NoError(t, err)

		finalSize := log.Size()
		require.Greater(t, finalSize, initSize)
		require.Greater(t, log.TotalWritten(), finalSize)
		require.Equal(t, finalSize, maxCircularLogSize)
		require.Equal(t, finalSize, log.Len())

		logBytes := log.Bytes()

		require.Equal(t, endSentinel, logBytes[len(logBytes)-len(endSentinel):])
		require.NotEqual(t, startSentinel, logBytes[:len(startSentinel)])
	})
	t.Run("overflow-write", func(t *testing.T) {
		log, err := circlog.NewCircularLog(maxCircularLogSize)
		require.NoError(t, err)

		initSize := log.Size()
		writeSize := maxCircularLogSize + 1
		n, err := writeFixedSize(log, writeSize)
		require.NoError(t, err)
		finalSize := log.Size()
		require.Less(t, n, writeSize)
		require.Greater(t, finalSize, initSize)
		require.Equal(t, finalSize, maxCircularLogSize)
		logBytes := log.Bytes()
		require.Equal(t, []byte("(...)"), logBytes[:5])
	})
	t.Run("stringify", func(t *testing.T) {
		log, err := circlog.NewCircularLog(maxCircularLogSize)
		require.NoError(t, err)

		writtenString := "A harmful truth is better than a useful lie."
		n, err := log.Write([]byte(writtenString))
		require.NoError(t, err)
		require.Equal(t, len(writtenString), n)
		loggedString := log.String()
		require.Equal(t, writtenString, loggedString)
	})
}
