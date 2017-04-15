package model

type Filesytem struct {
	Name      string
	Children  []Filesytem
	Snapshots []Snapshot
}

type FilesytemMapping struct {
	From Filesytem
	To   Filesytem
}

type Snapshot struct {
	Name string
}

type Pool struct {
	Root      Filesytem
}

type SSHTransport struct {
	Host string
	User string
	Port uint16
	TransportOpenCommand []string
	Options []string
}
