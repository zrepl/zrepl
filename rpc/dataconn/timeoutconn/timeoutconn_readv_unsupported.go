// +build illumos solaris

package timeoutconn

import "net"

func (c Conn) readv(buffers net.Buffers) (n int64, err error) {
	// Go does not expose the SYS_READV symbol for Solaris / Illumos - do they have it?
	// Anyhow, use the fallback
	return c.readvFallback(buffers)
}
