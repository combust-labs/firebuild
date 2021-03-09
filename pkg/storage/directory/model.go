package directory

type kernelResult struct {
	hostPath string
	metadata map[string]interface{}
}

func (r *kernelResult) HostPath() string {
	return r.hostPath
}

func (r *kernelResult) Metadata() map[string]interface{} {
	return r.metadata
}

type rootfsResult struct {
	hostPath string
	metadata map[string]interface{}
}

func (r *rootfsResult) HostPath() string {
	return r.hostPath
}

func (r *rootfsResult) Metadata() map[string]interface{} {
	return r.metadata
}
