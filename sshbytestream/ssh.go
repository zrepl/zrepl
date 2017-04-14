package sshbytestream

func Incoming() (io.ReadWriteCloser, error) {
	// derivce ReadWriteCloser from stdin & stdout
}

func Outgoing(name string, remote model.SSHTransport) (io.ReadWriteCloser, error) {
	// encapsulate
	//  fork(),exec(ssh $remote zrepl ipc ssh $name)
	// stdin and stdout in a ReadWriteCloser
	return ForkedSSHReadWriteCloser{}
}

struct ForkedSSHReadWriteCloser {

}

func (f ForkedSSHReadWriteCloser) Read(p []byte) (n int, err error) {

}

func (f ForkedSSHReadWriteCloser) Write(p []byte) (n int, err error) {

}

func (f ForkedSSHReadWriteCloser)  Close() error {

}