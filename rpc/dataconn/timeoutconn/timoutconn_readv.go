// +build !illumos
// +build !solaris

package timeoutconn

import (
	"io"
	"net"
	"syscall"
	"unsafe"
)

func buildIovecs(buffers net.Buffers) (totalLen int64, vecs []syscall.Iovec) {
	vecs = make([]syscall.Iovec, 0, len(buffers))
	for i := range buffers {
		totalLen += int64(len(buffers[i]))
		if len(buffers[i]) == 0 {
			continue
		}

		v := syscall.Iovec{
			Base: &buffers[i][0],
		}
		// syscall.Iovec.Len has platform-dependent size, thus use SetLen
		v.SetLen(len(buffers[i]))

		vecs = append(vecs, v)
	}
	return totalLen, vecs
}

func (c Conn) readv(buffers net.Buffers) (n int64, err error) {

	scc, ok := c.Wire.(SyscallConner)
	if !ok {
		return c.readvFallback(buffers)
	}
	rawConn, err := scc.SyscallConn()
	if err == SyscallConnNotSupported {
		return c.readvFallback(buffers)
	}
	if err != nil {
		return 0, err
	}

	_, iovecs := buildIovecs(buffers)

	for len(iovecs) > 0 {
		if err := c.renewReadDeadline(); err != nil {
			return n, err
		}
		oneN, oneErr := c.doOneReadv(rawConn, &iovecs)
		n += oneN
		if netErr, ok := oneErr.(net.Error); ok && netErr.Timeout() && oneN > 0 { // TODO likely not working
			continue
		} else if oneErr == nil && oneN > 0 {
			continue
		} else {
			return n, oneErr
		}
	}
	return n, nil
}

func (c Conn) doOneReadv(rawConn syscall.RawConn, iovecs *[]syscall.Iovec) (n int64, err error) {
	rawReadErr := rawConn.Read(func(fd uintptr) (done bool) {
		// iovecs, n and err must not be shadowed!

		// NOTE: unsafe.Pointer safety rules
		// 		https://tip.golang.org/pkg/unsafe/#Pointer
		//
		//		(4) Conversion of a Pointer to a uintptr when calling syscall.Syscall.
		// 		...
		//		uintptr() conversions must appear within the syscall.Syscall argument list.
		//      (even though we are not the escape analysis Likely not )
		thisReadN, _, errno := syscall.Syscall(
			syscall.SYS_READV,
			fd,
			uintptr(unsafe.Pointer(&(*iovecs)[0])),
			uintptr(len(*iovecs)),
		)
		if thisReadN == ^uintptr(0) {
			if errno == syscall.EAGAIN {
				return false
			}
			err = syscall.Errno(errno)
			return true
		}
		if int(thisReadN) < 0 {
			panic("unexpected return value")
		}
		n += int64(thisReadN) // TODO check overflow

		// shift iovecs forward
		for left := int(thisReadN); left > 0; {
			// conversion to uint does not change value, see TestIovecLenFieldIsMachineUint, and left > 0
			thisIovecConsumedCompletely := uint((*iovecs)[0].Len) <= uint(left)
			if thisIovecConsumedCompletely {
				// Update left, cannot go below 0 due to
				// a) definition of thisIovecConsumedCompletely
				// b) left > 0 due to loop invariant
				// Convertion .Len to int64 is thus also safe now, because it is < left < INT_MAX
				left -= int((*iovecs)[0].Len)
				*iovecs = (*iovecs)[1:]
			} else {
				// trim this iovec to remaining length

				// NOTE: unsafe.Pointer safety rules
				// 		https://tip.golang.org/pkg/unsafe/#Pointer
				// 		(3) Conversion of a Pointer to a uintptr and back, with arithmetic.
				// 		...
				//		Note that both conversions must appear in the same expression,
				//		with only the intervening arithmetic between them:
				(*iovecs)[0].Base = (*byte)(unsafe.Pointer(uintptr(unsafe.Pointer((*iovecs)[0].Base)) + uintptr(left)))
				curVecNewLength := uint((*iovecs)[0].Len) - uint(left) // casts to uint do not change value
				(*iovecs)[0].SetLen(int(curVecNewLength))              // int and uint have the same size, no change of value

				break // inner
			}
		}
		if thisReadN == 0 {
			err = io.EOF
			return true
		}
		return true
	})

	if rawReadErr != nil {
		err = rawReadErr
	}

	return n, err
}
