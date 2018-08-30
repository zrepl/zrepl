package zfs

const ReplicatedProperty = "zrepl:replicated"

// May return *DatasetDoesNotExist as an error
func ZFSGetReplicatedProperty(fs *DatasetPath, v *FilesystemVersion) (replicated bool, err error) {
	props, err := zfsGet(v.ToAbsPath(fs), []string{ReplicatedProperty})
	if err != nil {
		return false, err
	}
	if props.Get(ReplicatedProperty) == "yes" {
		return true, nil
	}
	return false, nil
}

func ZFSSetReplicatedProperty(fs *DatasetPath, v *FilesystemVersion, replicated bool) error {
	val := "no"
	if replicated {
		val = "yes"
	}
	props := NewZFSProperties()
	props.Set(ReplicatedProperty, val)
	return zfsSet(v.ToAbsPath(fs), props)
}
