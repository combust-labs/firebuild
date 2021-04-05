package containers

// DockerImageMetadata contains the Docker image manifest and config.
type DockerImageMetadata struct {
	Manifest *DockerImageManifest
	Config   *DockerImageConfig
}

// DockerImageManifest is the Docker image manifest.
type DockerImageManifest struct {
	Config   string   `json:"Config" mapstructure:"Config"`
	Layers   []string `json:"Layers" mapstructure:"Layers"`
	RepoTags []string `json:"RepoTags" mapstructure:"RepoTags"`
}

// DockerImageConfig is the Docker image config.
type DockerImageConfig struct {
	Architecture    string                     `json:"architecture" mapstructure:"architecture"`
	Config          *DockerImageConfigConfig   `json:"config" mapstructure:"config"`
	Container       string                     `json:"container" mapstructure:"container"`
	ContainerConfig *DockerImageConfigConfig   `json:"container_config" mapstructure:"container_config"`
	Created         string                     `json:"created" mapstructure:"created"`
	DockerVersion   string                     `json:"docker_version" mapstructure:"docker_version"`
	History         []*DockerImageHistoryEntry `json:"history" mapstructure:"history"`
	Os              string                     `json:"os" mapstructure:"os"`
	Rootfs          interface{}                `json:"rootfs" mapstructure:"rootfs"`
}

// DockerImageConfigConfig is the Docker image config Config and ContainerConfig configuration.
type DockerImageConfigConfig struct {
	AttachStderr bool                   `json:"AttachStderr" mapstructure:"AttachStderr"`
	AttachStdin  bool                   `json:"AttachStdin" mapstructure:"AttachStdin"`
	AttachStdout bool                   `json:"AttachStdout" mapstructure:"AttachStdout"`
	Cmd          []string               `json:"Cmd" mapstructure:"Cmd"`
	Domainname   string                 `json:"Domainname" mapstructure:"Domainname"`
	Entrypoint   []string               `json:"Entrypoint" mapstructure:"Entrypoint"`
	Env          []string               `json:"Env" mapstructure:"Env"`
	ExposedPorts map[string]interface{} `json:"ExposedPorts" mapstructure:"ExposedPorts"`
	Hostname     string                 `json:"Hostname" mapstructure:"Hostname"`
	Image        string                 `json:"Image" mapstructure:"Image"`
	Labels       map[string]string      `json:"Labels" mapstructure:"Labels"`
	OnBuild      interface{}            `json:"OnBuild" mapstructure:"OnBuild"`
	OpenStdin    bool                   `json:"OpenStdin" mapstructure:"OpenStdin"`
	StdinOnce    bool                   `json:"StdinOnce" mapstructure:"StdinOnce"`
	StopSignal   string                 `json:"StopSignal" mapstructure:"StopSignal"`
	Tty          bool                   `json:"Tty" mapstructure:"Tty"`
	User         string                 `json:"User" mapstructure:"User"`
	Volumes      map[string]interface{} `json:"Volumes" mapstructure:"Volumes"`
	WorkingDir   string                 `json:"WorkingDir" mapstructure:"WorkingDir"`
}

// DockerImageHistoryEntry is the Docker image history entry.
type DockerImageHistoryEntry struct {
	Created    string `json:"created" mapstructure:"created"`
	CreatedBy  string `json:"created_by" mapstructure:"created_by"`
	EmptyLayer bool   `json:"empty_layer" mapstructure:"empty_layer"`
}
