package build

import "github.com/combust-labs/firebuild/pkg/build/commands"

type EntrypointInfo struct {
	Cmd        commands.Cmd        `json:"cmd" mapstructure:"cmd"`
	Entrypoint commands.Entrypoint `json:"entrypoint" mapstructure:"entrypoint"`
}
