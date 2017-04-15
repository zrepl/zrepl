package model

type Filesystem struct {
	Name      string
	Children  []Filesystem
	Snapshots []Snapshot
}

type FilesytemMapping struct {
	From Filesystem
	To   Filesystem
}

type Snapshot struct {
	Name string
}

type Pool struct {
	Root Filesystem
}

type SSHTransport struct {
	Host string
	User string
	Port uint16
	TransportOpenCommand []string
	Options []string
}
