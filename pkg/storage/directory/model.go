package directory

type kernelResult struct {
	hostPath string
	metadata map[string]interface{}
}

func (r *kernelResult) HostPath() string {
	return r.hostPath
}

func (r *kernelResult) Metadata() interface{} {
	return r.metadata
}

type rootfsResult struct {
	hostPath string
	metadata interface{}
}

func (r *rootfsResult) HostPath() string {
	return r.hostPath
}

func (r *rootfsResult) Metadata() interface{} {
	return r.metadata
}
