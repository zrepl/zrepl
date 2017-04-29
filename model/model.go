package model

type Filesystem struct {
	Name      string
	Parent    *Filesystem
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
