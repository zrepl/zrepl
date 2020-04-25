package trace

import (
	"encoding/base64"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/zrepl/zrepl/util/envconst"
)

var genIdPRNG = rand.New(rand.NewSource(1))

func init() {
	genIdPRNG.Seed(time.Now().UnixNano())
	genIdPRNG.Seed(int64(os.Getpid()))
}

var genIdNumBytes = envconst.Int("ZREPL_TRACE_ID_NUM_BYTES", 3)

func init() {
	if genIdNumBytes < 1 {
		panic("trace node id byte length must be at least 1")
	}
}

func genID() string {
	var out strings.Builder
	enc := base64.NewEncoder(base64.RawStdEncoding, &out)
	buf := make([]byte, genIdNumBytes)
	for i := 0; i < len(buf); {
		n, err := genIdPRNG.Read(buf[i:])
		if err != nil {
			panic(err)
		}
		i += n
	}
	n, err := enc.Write(buf[:])
	if err != nil || n != len(buf) {
		panic(err)
	}
	if err := enc.Close(); err != nil {
		panic(err)
	}
	return out.String()
}
