package buildcontext

import (
	"os"

	"github.com/appministry/firebuild/buildcontext/commands"
	"github.com/appministry/firebuild/buildcontext/resources"
	"github.com/appministry/firebuild/remote"
	"github.com/hashicorp/go-hclog"
)

// Build represents the build operation.
type Build interface {
	Build(remote.ConnectedClient, ...commands.Run) error
	ExposedPorts() []string
	From() *commands.From
	Metadata() map[string]string
	WithLogger(hclog.Logger) Build
	WithResolver(resources.Resolver) Build
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
	logger            hclog.Logger
	resolver          resources.Resolver

	resolvedResources map[string]resources.ResolvedResource
}

func (b *defaultBuild) Build(remoteClient remote.ConnectedClient, initCommands ...commands.Run) error {

	buildLogger := b.logger.With("from", b.from.BaseImage)

	// validate resources first:
	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case commands.Add:
			resolvedResource, err := b.resolver.ResolveAdd(tcommand)
			if err != nil {
				return err
			}
			b.resolvedResources[tcommand.Source] = resolvedResource
		case commands.Copy:
			resolvedResource, err := b.resolver.ResolveCopy(tcommand)
			if err != nil {
				return err
			}
			b.resolvedResources[tcommand.Source] = resolvedResource
		}
	}

	for _, initCommand := range initCommands {
		if err := remoteClient.RunCommand(initCommand); err != nil {
			return err
		}
	}
	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case commands.Add:
			if resource, ok := b.resolvedResources[tcommand.Source]; ok {
				buildLogger.Info("Putting ADD resource", "source", tcommand.Source)
				if err := remoteClient.PutResource(resource); err != nil {
					buildLogger.Error("PutResource ADD resource failed", "source", tcommand.Source, "reason", err)
					return err
				}
			} else {
				buildLogger.Error("resource ADD type required but not resolved", "source", tcommand.Source)
			}
		case commands.Copy:
			buildLogger.Info("Putting COPY resource", "source", tcommand.Source)
			if resource, ok := b.resolvedResources[tcommand.Source]; ok {
				if err := remoteClient.PutResource(resource); err != nil {
					buildLogger.Error("PutResource COPY resource failed", "source", tcommand.Source, "reason", err)
					return err
				}
			} else {
				buildLogger.Error("resource COPY type required but not resolved", "source", tcommand.Source)
			}
		case commands.Run:
			if err := remoteClient.RunCommand(tcommand); err != nil {
				buildLogger.Error("RunCommand failed", "reason", err)
				return err
			}
		}
	}
	/*
		fmt.Println("Metadata is", b.Metadata())
		fmt.Println("Exposed ports", b.ExposedPorts())
		fmt.Println("OS Service should execute:")
		fmt.Println(" =====> Command: ", b.currentEntrypoint.Values)
		fmt.Println(" =====> Arguments: ", b.currentCmd)
		fmt.Println(fmt.Sprintf(" =====> As user %q, Using shell %q, in directory %q", b.currentEntrypoint.User.Value, b.currentEntrypoint.Shell.Commands, b.currentEntrypoint.Workdir.Value))
	*/
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

func (b *defaultBuild) WithLogger(input hclog.Logger) Build {
	b.logger = input
	return b
}

func (b *defaultBuild) WithResolver(input resources.Resolver) Build {
	b.resolver = input
	return b
}

func (b *defaultBuild) WithFrom(input *commands.From) Build {
	b.from = input
	return b
}

func (b *defaultBuild) WithInstruction(input interface{}) Build {
	switch tinput := input.(type) {
	case commands.Add:
		tinput.User = b.currentUser
		tinput.Workdir = b.currentWorkdir
		b.instructions = append(b.instructions, tinput)
	case commands.Arg:
		b.currentArgs[tinput.Name] = tinput.Value
	case commands.Cmd:
		b.currentCmd = tinput
	case commands.Copy:
		tinput.User = b.currentUser
		tinput.Workdir = b.currentWorkdir
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
		logger:            hclog.Default(),
		resolver:          resources.NewDefaultResolver(),
		resolvedResources: map[string]resources.ResolvedResource{},
	}
}

func defaultResourceAddResolve(res commands.Add) (*os.File, error) {
	return defaultResourceResolve(res.Source)
}

func defaultResourceCopyResolve(res commands.Copy) (*os.File, error) {
	return defaultResourceResolve(res.Source)
}

func defaultResourceResolve(source string) (*os.File, error) {

	return nil, nil
}
