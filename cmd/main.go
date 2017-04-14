package main

type Role uint

const (
	ROLE_IPC Role = iota
	ROLE_ACTION Role = iota
)

func main() {

	role = ROLE_IPC // TODO: if argv[1] == ipc then...
	switch (role) {
	case ROLE_IPC:
		doIPC()
	case ROLE_ACTION:
		doAction()
	}

}

func doIPC() {

	sshByteStream = sshbytestream.Incoming()
	handler = Handler{}
	if err := ListenByteStreamRPC(sshByteStream, handler); err != nil {
		// PANIC
	}

	// exit(0)

}

func doAction() {

	sshByteStream = sshbytestream.Outgoing(model.SSHTransport{})

	remote,_ := ConnectByteStreamRPC(sshByteStream)

	request := NewFilesystemRequest(["zroot/var/db", "zroot/home"])
	forest, _ := remote.FilesystemRequest(request)

	for tree := forest {
		fmt.Println(tree)
	}

}


type Handler struct {}

func (h Handler) HandleFilesystemRequest(r FilesystemRequest) (roots []model.Filesystem, err error) {

	roots = make([]model.Filesystem, 0, 10)

	for _, root := range r.Roots {
		if zfsRoot, err := zfs.FilesystemsAtRoot(root); err != nil {
			return nil, err
		}
		roots = append(roots, zfsRoot)
	}

	return
}

func (h Handler) HandleInitialTransferRequest(r InitialTransferRequest) (io.Read, error) {
	// TODO ACL
	return zfs.InitialSend(r.Snapshot)
}
func (h Handler) HandleIncrementalTransferRequestRequest(r IncrementalTransferRequest) (io.Read, error) {
	// TODO ACL
	return zfs.IncrementalSend(r.FromSnapshot, r.ToSnapshot)
}
