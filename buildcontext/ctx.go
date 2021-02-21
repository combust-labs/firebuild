package buildcontext

import (
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/appministry/firebuild/buildcontext/commands"
	"github.com/appministry/firebuild/buildcontext/resources"
	"github.com/appministry/firebuild/remote"
	"github.com/hashicorp/go-hclog"
)

const etcDirectory = "/etc/firebuild"

// Build represents the build operation.
type Build interface {
	Build(remote.ConnectedClient) error
	ExposedPorts() []string
	From() *commands.From
	Metadata() map[string]string
	WithFrom(*commands.From) Build
	WithInstruction(interface{}) Build
	WithLogger(hclog.Logger) Build
	WithPostBuildCommands(...commands.Run) Build
	WithPreBuildCommands(...commands.Run) Build
	WithResolver(resources.Resolver) Build
	WithServiceInstaller(string) Build
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
	serviceInstaller  string

	postBuildCommands []commands.Run
	preBuildCommands  []commands.Run

	resolvedResources map[string]resources.ResolvedResource
}

func (b *defaultBuild) Build(remoteClient remote.ConnectedClient) error {

	b.logger.Info("building from", "base", b.from.BaseImage)

	// validate resources first:
	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case commands.Add:
			resolvedResource, err := b.resolver.ResolveAdd(tcommand)
			if err != nil {
				b.logger.Error("failed resolving ADD resource", "reason", err)
				return err
			}
			b.resolvedResources[tcommand.Source] = resolvedResource
		case commands.Copy:
			resolvedResource, err := b.resolver.ResolveCopy(tcommand)
			if err != nil {
				b.logger.Error("failed resolving COPY resource", "reason", err)
				return err
			}
			b.resolvedResources[tcommand.Source] = resolvedResource
		}
	}

	for _, cmd := range b.preBuildCommands {
		if err := remoteClient.RunCommand(cmd); err != nil {
			return err
		}
	}

	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case commands.Add:
			if resource, ok := b.resolvedResources[tcommand.Source]; ok {
				b.logger.Info("Putting ADD resource", "source", tcommand.Source)
				if err := remoteClient.PutResource(resource); err != nil {
					b.logger.Error("PutResource ADD resource failed", "source", tcommand.Source, "reason", err)
					return err
				}
			} else {
				b.logger.Error("resource ADD type required but not resolved", "source", tcommand.Source)
			}
		case commands.Copy:
			b.logger.Info("Putting COPY resource", "source", tcommand.Source)
			if resource, ok := b.resolvedResources[tcommand.Source]; ok {
				if err := remoteClient.PutResource(resource); err != nil {
					b.logger.Error("PutResource COPY resource failed", "source", tcommand.Source, "reason", err)
					return err
				}
			} else {
				b.logger.Error("resource COPY type required but not resolved", "source", tcommand.Source)
			}
		case commands.Run:
			if err := remoteClient.RunCommand(tcommand); err != nil {
				b.logger.Error("RunCommand failed", "reason", err)
				return err
			}
		case commands.Volume:
			for _, vol := range tcommand.Values {
				run := commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s'", vol))
				run.User = tcommand.User
				run.Workdir = tcommand.Workdir
				if err := remoteClient.RunCommand(run); err != nil {
					b.logger.Error("RunCommand for VOLUME command failed", "reason", err)
					return err
				}
			}
		}
	}

	// always create the service env file:
	serviceEnv := []string{
		fmt.Sprintf("SERVICE_WORKDIR=\"%s\"", b.currentEntrypoint.Workdir.Value),
		fmt.Sprintf("SERVICE_SHELL=\"%s\"", strings.Join(mapShellString(b.currentEntrypoint.Shell.Commands), " ")),
		fmt.Sprintf("SERVICE_ENTRYPOINT=\"%s\"", strings.Join(mapShellString(b.currentEntrypoint.Values), " ")),
		fmt.Sprintf("SERVICE_CMDS=\"%s\"", strings.Join(mapShellString(b.currentCmd.Values), " ")),
		fmt.Sprintf("SERVICE_USER=\"%s\"", b.currentEntrypoint.User.Value),
	}

	b.logger.Info("Creating bootstrap data location", "location", etcDirectory)

	if err := remoteClient.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s'", etcDirectory))); err != nil {
		b.logger.Error("BOOTSTRAP INCOMPLETE: Failed creating firebuild bootstrap data directory",
			"directory", etcDirectory,
			"reason", err,
			"service-env", serviceEnv)
		return err
	}

	b.logger.Info("Uploading the service environment file")

	if err := remoteClient.PutResource(resources.
		NewResolvedFileResource([]byte(strings.Join(serviceEnv, "\n")+"\n"),
			fs.FileMode(0644),
			filepath.Join(etcDirectory, "cmd.env"),
			commands.DefaultWorkdir(),
			commands.DefaultUser())); err != nil {
		b.logger.Error("BOOTSTRAP INCOMPLETE: Failed uploading the service environment file",
			"reason", err,
			"service-env", serviceEnv)
		return err
	}

	// if we succeeded and we have the installer file ... but only if we have a file...
	if b.serviceInstaller != "" {

		b.logger.Info("Validating service installer file")

		// check if it exists and is a file:
		stat, statErr := os.Stat(b.serviceInstaller)
		if statErr != nil {
			b.logger.Error("BOOTSTRAP INCOMPLETE: Failed service installer stat",
				"reason", statErr,
				"service-env", serviceEnv)
			return statErr
		}
		if stat.IsDir() {
			b.logger.Error("BOOTSTRAP INCOMPLETE: Service installer points to a directory",
				"service-env", serviceEnv)
			return fmt.Errorf("directory: expected file: %s", b.serviceInstaller)
		}
		installerBytes, readErr := ioutil.ReadFile(b.serviceInstaller)
		if readErr != nil && readErr != io.EOF {
			b.logger.Error("BOOTSTRAP INCOMPLETE: Failed reading the installer file",
				"reason", readErr,
				"service-env", serviceEnv)
			return readErr
		}

		b.logger.Info("Uploading service installer file")

		// upload the installer:
		installerPath := filepath.Join(etcDirectory, "installer.sh")
		if err := remoteClient.PutResource(resources.
			NewResolvedFileResource([]byte(installerBytes),
				fs.FileMode(0754),
				installerPath,
				commands.DefaultWorkdir(),
				commands.DefaultUser())); err != nil {
			b.logger.Error("Failed uploading the service environment file",
				"reason", err,
				"service-env", serviceEnv)
			return err
		}

		b.logger.Info("Executing service installer file")

		// execute the installer, we leave it there...:
		if err := remoteClient.RunCommand(commands.RunWithDefaults(installerPath)); err != nil {
			b.logger.Error("BOOTSTRAP INCOMPLETE: Failed executing the local service installer",
				"installer-path", installerPath,
				"reason", err,
				"service-env", serviceEnv)
			return err
		}

		b.logger.Info("Service installer file executed")

	} else {
		b.logger.Warn("No service installer file configured")
	}

	for _, cmd := range b.postBuildCommands {
		if err := remoteClient.RunCommand(cmd); err != nil {
			return err
		}
	}

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
	case commands.Volume:
		tinput.User = b.currentUser
		tinput.Workdir = b.currentWorkdir
		b.instructions = append(b.instructions, tinput)
	case commands.Workdir:
		b.currentWorkdir = tinput
	}

	return b
}

func (b *defaultBuild) WithLogger(input hclog.Logger) Build {
	b.logger = input
	return b
}

func (b *defaultBuild) WithPostBuildCommands(cmds ...commands.Run) Build {
	b.postBuildCommands = append(b.postBuildCommands, cmds...)
	return b
}
func (b *defaultBuild) WithPreBuildCommands(cmds ...commands.Run) Build {
	b.preBuildCommands = append(b.preBuildCommands, cmds...)
	return b
}

func (b *defaultBuild) WithResolver(input resources.Resolver) Build {
	b.resolver = input
	return b
}

func (b *defaultBuild) WithServiceInstaller(input string) Build {
	b.serviceInstaller = input
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

func mapShellString(input []string) []string {
	for idx, item := range input {
		input[idx] = strings.ReplaceAll(item, "\"", "\"\"")
	}
	return input
}
