package build

import "github.com/combust-labs/firebuild/pkg/build/commands"

// EntrypointInfo contains the Docker entrypoint and commands.
// Returned by the rootfs builder after parsing the Docker source.
// Used primarily for metadata.
type EntrypointInfo struct {
	Cmd        commands.Cmd        `json:"cmd" mapstructure:"cmd"`
	Entrypoint commands.Entrypoint `json:"entrypoint" mapstructure:"entrypoint"`
}
