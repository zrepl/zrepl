package model

type Filesytem struct {
	Name      string
	Children  []Filesytem
	Snapshots []Snaphot
}

type FilesytemMapping struct {
	From Filesytem
	To   Filesytem
}

type Snapshot struct {
	Name String
}

type Pool struct {
	Transport Transport
	Root      Filesytem
}

type Transport interface {
	Connect() Connection
}

type SSHTransport struct {
	Host string
	User string
	Port uint16
}

type LocalTransport struct{}
