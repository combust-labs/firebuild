package buildcontext

import (
	"fmt"

	"github.com/appministry/firebuild/buildcontext/commands"
	"github.com/appministry/firebuild/remote"
)

// Build represents the build operation.
type Build interface {
	Build(remote.ConnectedClient, ...commands.Run) error
	ExposedPorts() []string
	From() *commands.From
	Metadata() map[string]string
	WithFrom(*commands.From) Build
	WithInstruction(interface{}) Build
}

type defaultBuild struct {
	currentArgs       map[string]string
	currentCmd        commands.Cmd
	currentEntrypoint commands.Entrypoint
	currentEnv        map[string]string
	currentMetadata   map[string]string
	currentShell      commands.Shell
	currentUser       commands.User
	currentWorkdir    commands.Workdir
	exposedPorts      []string
	from              *commands.From
	instructions      []interface{}
}

func (b *defaultBuild) Build(remoteClient remote.ConnectedClient, initCommands ...commands.Run) error {
	fmt.Println("Building rootfs from", b.from.BaseImage)
	for _, initCommand := range initCommands {
		if err := remoteClient.RunCommand(initCommand); err != nil {
			return err
		}
	}
	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case commands.Add:
			fmt.Println(fmt.Sprintf(" =====> Add %q as %q", tcommand.Source, tcommand.Target))
		case commands.Copy:
			fmt.Println(fmt.Sprintf(" =====> Copy %q as %q", tcommand.Source, tcommand.Target))
		case commands.Run:
			if err := remoteClient.RunCommand(tcommand); err != nil {
				return err
			}
		}
	}
	fmt.Println("Metadata is", b.Metadata())
	fmt.Println("Exposed ports", b.ExposedPorts())
	fmt.Println("OS Service should execute:")
	fmt.Println(" =====> Command: ", b.currentEntrypoint.Values)
	fmt.Println(" =====> Arguments: ", b.currentCmd)
	fmt.Println(fmt.Sprintf(" =====> As user %q, Using shell %q, in directory %q", b.currentEntrypoint.User.Value, b.currentEntrypoint.Shell.Commands, b.currentEntrypoint.Workdir.Value))
	return nil
}

func (b *defaultBuild) ExposedPorts() []string {
	return b.exposedPorts
}

func (b *defaultBuild) From() *commands.From {
	if b.from == nil {
		return nil
	}
	return &commands.From{BaseImage: b.from.BaseImage}
}

func (b *defaultBuild) Metadata() map[string]string {
	return b.currentMetadata
}

func (b *defaultBuild) WithFrom(input *commands.From) Build {
	b.from = input
	return b
}

func (b *defaultBuild) WithInstruction(input interface{}) Build {
	switch tinput := input.(type) {
	case commands.Add:
		b.instructions = append(b.instructions, tinput)
	case commands.Arg:
		b.currentArgs[tinput.Name] = tinput.Value
	case commands.Cmd:
		b.currentCmd = tinput
	case commands.Copy:
		b.instructions = append(b.instructions, tinput)
	case commands.Entrypoint:
		tinput.Env = b.currentEnv
		tinput.Shell = b.currentShell
		tinput.User = b.currentUser
		tinput.Workdir = b.currentWorkdir
		b.currentEntrypoint = tinput
	case commands.Env:
		b.currentEnv[tinput.Name] = tinput.Value
	case commands.Expose:
		b.exposedPorts = append(b.exposedPorts, tinput.RawValue)
	case commands.Label:
		b.currentMetadata[tinput.Key] = tinput.Value
	case commands.Run:
		tinput.Args = b.currentArgs
		tinput.Env = b.currentEnv
		tinput.Shell = b.currentShell
		tinput.User = b.currentUser
		tinput.Workdir = b.currentWorkdir
		b.instructions = append(b.instructions, tinput)
	case commands.Shell:
		b.currentShell = tinput
	case commands.User:
		b.currentUser = tinput
	case commands.Workdir:
		b.currentWorkdir = tinput
	}

	return b
}

// NewDefaultBuild returns an instance of the default Build implementation.
func NewDefaultBuild() Build {
	return &defaultBuild{
		currentArgs:       map[string]string{},
		currentCmd:        commands.Cmd{Values: []string{}},
		currentEntrypoint: commands.Entrypoint{Values: []string{}},
		currentEnv:        map[string]string{},
		currentMetadata:   map[string]string{},
		currentShell:      commands.Shell{Commands: []string{"/bin/sh", "-c"}},
		currentUser:       commands.User{Value: "0:0"},
		currentWorkdir:    commands.Workdir{Value: "/"},
		exposedPorts:      []string{},
		instructions:      []interface{}{},
	}
}
