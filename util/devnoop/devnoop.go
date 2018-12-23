// package devnoop provides an io.ReadWriteCloser that never errors
// and always reports reads / writes to / from buffers as complete.
// The buffers themselves are never touched.
package devnoop

type Dev struct{}

func Get() Dev {
	return Dev{}
}

func (Dev) Write(p []byte) (n int, err error) { return len(p), nil }
func (Dev) Read(p []byte) (n int, err error)  { return len(p), nil }
func (Dev) Close() error                      { return nil }
