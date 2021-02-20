package buildcontext

import (
	"fmt"
)

// Build represents the build operation.
type Build interface {
	Build() error
	ExposedPorts() []string
	Metadata() map[string]string
	WithFrom(*From) Build
	WithInstruction(interface{}) Build
}

type defaultBuild struct {
	currentArgs       map[string]string
	currentCmd        Cmd
	currentEntrypoint Entrypoint
	currentEnv        map[string]string
	currentMetadata   map[string]string
	currentShell      Shell
	currentUser       User
	currentWorkdir    Workdir
	exposedPorts      []string
	from              *From
	instructions      []interface{}
}

func (b *defaultBuild) Build() error {
	fmt.Println("Building rootfs from", b.from.BaseImage)
	fmt.Println("Metadata is", b.Metadata())
	fmt.Println("Exposed ports", b.ExposedPorts())
	fmt.Println("Commands to run")
	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case Add:
			fmt.Println(fmt.Sprintf(" =====> Add %q as %q", tcommand.Source, tcommand.Target))
		case Copy:
			fmt.Println(fmt.Sprintf(" =====> Copy %q as %q", tcommand.Source, tcommand.Target))
		case Run:
			fmt.Println(fmt.Sprintf(" =====> Run %q as %q using shell %q in directory %q", tcommand.Command, tcommand.User.Value, tcommand.Shell.Commands, tcommand.Workdir.Value))
		}
	}
	fmt.Println("OS Service should execute:")
	fmt.Println(" =====> Command: ", b.currentEntrypoint.Values)
	fmt.Println(" =====> Arguments: ", b.currentCmd)
	fmt.Println(fmt.Sprintf(" =====> As user %q, Using shell %q, in directory %q", b.currentEntrypoint.User.Value, b.currentEntrypoint.Shell.Commands, b.currentEntrypoint.Workdir.Value))
	return nil
}

func (b *defaultBuild) ExposedPorts() []string {
	return b.exposedPorts
}

func (b *defaultBuild) Metadata() map[string]string {
	return b.currentMetadata
}

func (b *defaultBuild) WithFrom(input *From) Build {
	b.from = input
	return b
}

func (b *defaultBuild) WithInstruction(input interface{}) Build {
	switch tinput := input.(type) {
	case Add:
		b.instructions = append(b.instructions, tinput)
	case Arg:
		b.currentArgs[tinput.Name] = tinput.Value
	case Cmd:
		b.currentCmd = tinput
	case Copy:
		b.instructions = append(b.instructions, tinput)
	case Entrypoint:
		tinput.Env = b.currentEnv
		tinput.Shell = b.currentShell
		tinput.User = b.currentUser
		tinput.Workdir = b.currentWorkdir
		b.currentEntrypoint = tinput
	case Env:
		b.currentEnv[tinput.Name] = tinput.Value
	case Expose:
		b.exposedPorts = append(b.exposedPorts, tinput.RawValue)
	case Label:
		b.currentMetadata[tinput.Key] = tinput.Value
	case Run:
		tinput.Args = b.currentArgs
		tinput.Env = b.currentEnv
		tinput.Shell = b.currentShell
		tinput.User = b.currentUser
		tinput.Workdir = b.currentWorkdir
		b.instructions = append(b.instructions, tinput)
	case Shell:
		b.currentShell = tinput
	case User:
		b.currentUser = tinput
	case Workdir:
		b.currentWorkdir = tinput
	}

	return b
}

// NewDefaultBuild returns an instance of the default Build implementation.
func NewDefaultBuild() Build {
	return &defaultBuild{
		currentArgs:       map[string]string{},
		currentCmd:        Cmd{Values: []string{}},
		currentEntrypoint: Entrypoint{Values: []string{}},
		currentEnv:        map[string]string{},
		currentMetadata:   map[string]string{},
		currentShell:      Shell{Commands: []string{"/bin/sh", "-c"}},
		currentUser:       User{Value: "0:0"},
		currentWorkdir:    Workdir{Value: "/"},
		exposedPorts:      []string{},
		instructions:      []interface{}{},
	}
}

// -- Instructions:

// Add represents the ADD instruction.
type Add struct {
	Source string
	Target string
}

// Arg represents the ARG instruction.
type Arg struct {
	Name  string
	Value string
}

// Cmd represents the CMD instruction.
type Cmd struct {
	Values []string
}

// Copy represents the COPY instruction.
type Copy struct {
	Source string
	Target string
}

// Entrypoint represents the ENTRYPOINT instruction.
type Entrypoint struct {
	Values  []string
	Env     map[string]string
	Shell   Shell
	Workdir Workdir
	User    User
}

// Env represents the ENV instruction.
type Env struct {
	Name  string
	Value string
}

// Expose represents the EXPOSE instruction.
type Expose struct {
	RawValue string
}

// From represents the FROM instruction.
type From struct {
	BaseImage string
}

// Label represents the LABEL instruction.
type Label struct {
	Key   string
	Value string
}

// Run represents the RUN instruction.
type Run struct {
	Args    map[string]string
	Command string
	Env     map[string]string
	Shell   Shell
	Workdir Workdir
	User    User
}

// Shell represents the SHELL instruction.
type Shell struct {
	Commands []string
}

// User represents the USER instruction.
type User struct {
	Value string
}

// Workdir represents the WORKDIR instruction.
type Workdir struct {
	Value string
}
