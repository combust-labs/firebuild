package build

import "github.com/combust-labs/firebuild-shared/build/commands"

// EntrypointInfo contains the Docker entrypoint and commands.
// Returned by the rootfs builder after parsing the Docker source.
// Used primarily for metadata.
type EntrypointInfo struct {
	Cmd        commands.Cmd        `json:"Cmd" mapstructure:"Cmd"`
	Entrypoint commands.Entrypoint `json:"Entrypoint" mapstructure:"Entrypoint"`
}
