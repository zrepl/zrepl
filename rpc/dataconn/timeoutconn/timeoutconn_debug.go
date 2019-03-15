package timeoutconn

import (
	"fmt"
	"io"
)

const debugReadvNoShortReadsAssertEnable = false

func debugReadvNoShortReadsAssert(expectedLen, returnedLen int64, returnedErr error) {
	readShort := expectedLen != returnedLen
	if !readShort {
		return
	}
	if returnedErr != io.EOF {
		return
	}
	panic(fmt.Sprintf("ReadvFull short and error is not EOF%v\n", returnedErr))
}
