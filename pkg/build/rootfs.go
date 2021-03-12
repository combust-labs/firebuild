package build

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/env"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/remote"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/hashicorp/go-hclog"
)

// Build represents the build operation.
type Build interface {
	AddInstructions(...interface{}) error
	Build(remote.ConnectedClient) error
	ExposedPorts() []string
	From() commands.From
	Metadata() map[string]string
	WithBuildArgs(map[string]string) Build
	WithDependencyResources(map[string][]resources.ResolvedResource) Build
	WithExcludes([]string) Build
	WithLogger(hclog.Logger) Build
	WithPostBuildCommands(...commands.Run) Build
	WithPreBuildCommands(...commands.Run) Build
	WithResolver(resources.Resolver) Build
	WithServiceInstaller(string) Build
}

type defaultBuild struct {
	buildArgs          map[string]string
	buildEnv           env.BuildEnv
	currentArgs        map[string]string
	currentCmd         commands.Cmd
	currentEntrypoint  commands.Entrypoint
	currentEnv         map[string]string
	currentMetadata    map[string]string
	currentShell       commands.Shell
	currentUser        commands.User
	currentWorkdir     commands.Workdir
	dependentResources map[string][]resources.ResolvedResource
	exposedPorts       []string
	excludes           []string
	from               commands.From
	instructions       []interface{}
	isDependencyBuild  bool
	logger             hclog.Logger
	resolver           resources.Resolver
	serviceInstaller   string

	postBuildCommands []commands.Run
	preBuildCommands  []commands.Run

	resolvedResources map[string][]resources.ResolvedResource
}

func (b *defaultBuild) Build(remoteClient remote.ConnectedClient) error {

	patternMatcher, matcherCreateErr := fileutils.NewPatternMatcher(b.excludes)
	if matcherCreateErr != nil {
		b.logger.Error("failed creating excludes pattern matcher", "reason", matcherCreateErr)
		return matcherCreateErr
	}

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

				pathMatched, matchErr := patternMatcher.Matches(tcommand.Source)
				if matchErr != nil {
					b.logger.Warn("error while matching path for PutResource ADD, skipping", "source", tcommand.Source, "reason", matchErr)
					continue // skip this resource
				}
				if pathMatched {
					// file to be excluded
					b.logger.Debug("skipping excluded path for PutResource ADD", "source", tcommand.Source)
					continue
				}

				b.logger.Info("Putting ADD resource", "source", tcommand.Source)
				for _, resourceItem := range resource {
					if err := remoteClient.PutResource(resourceItem); err != nil {
						b.logger.Error("PutResource ADD resource failed", "source", tcommand.Source, "reason", err)
						return err
					}
				}
			} else {
				b.logger.Error("resource ADD type required but not resolved", "source", tcommand.Source)
			}
		case commands.Copy:

			pathMatched, matchErr := patternMatcher.Matches(tcommand.Source)
			if matchErr != nil {
				b.logger.Warn("error while matching path for PutResource COPY, skipping", "source", tcommand.Source, "reason", matchErr)
				continue // skip this resource
			}
			if pathMatched {
				// file to be excluded
				b.logger.Debug("skipping excluded path for PutResource COPY", "source", tcommand.Source)
				continue
			}

			// dependency resources exist for COPY commands only:
			if tcommand.Stage != "" {
				// we need to locate a dependency resource
				dependencyResources, ok := b.dependentResources[tcommand.Stage]
				if !ok {
					b.logger.Error("PutResource COPY resource failed, no dependency resource stage", "source", tcommand.Source, "stage", tcommand.Stage)
					return fmt.Errorf("no dependency stage %s", tcommand.Stage)
				}
				resourceWasProcessed := false
				for _, dependencyResource := range dependencyResources {
					if strings.HasPrefix(dependencyResource.SourcePath(), tcommand.Source) {
						b.logger.Info("Putting COPY resource from dependency", "source", tcommand.Source, "stage", tcommand.Stage)
						if err := remoteClient.PutResource(dependencyResource); err != nil {
							b.logger.Error("PutResource COPY resource failed", "source", tcommand.Source, "stage", tcommand.Stage, "reason", err)
							return err
						}
						resourceWasProcessed = true
					}
				}
				if !resourceWasProcessed {
					b.logger.Error("resource COPY type required from stage but not resolved", "source", tcommand.Source, "stage", tcommand.Stage)
				}
				continue
			}

			b.logger.Info("Putting COPY resource", "source", tcommand.Source)
			if resource, ok := b.resolvedResources[tcommand.Source]; ok {
				for _, resourceItem := range resource {
					if err := remoteClient.PutResource(resourceItem); err != nil {
						b.logger.Error("PutResource COPY resource failed", "source", tcommand.Source, "reason", err)
						return err
					}
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
		fmt.Sprintf("export SERVICE_WORKDIR=\"%s\"", b.currentEntrypoint.Workdir.Value),
		fmt.Sprintf("export SERVICE_SHELL=\"%s\"", strings.Join(mapShellString(b.currentEntrypoint.Shell.Commands), " ")),
		fmt.Sprintf("export SERVICE_ENTRYPOINT=\"%s\"", strings.Join(mapShellString(b.currentEntrypoint.Values), " ")),
		fmt.Sprintf("export SERVICE_CMDS=\"%s\"", strings.Join(mapShellString(b.currentCmd.Values), " ")),
		fmt.Sprintf("export SERVICE_USER=\"%s\"", b.currentEntrypoint.User.Value),
	}

	// put gathered environment in the env file:
	for k, v := range b.currentEnv {
		serviceEnv = append(serviceEnv, fmt.Sprintf("export %s=\"%s\"", k, v))
	}

	b.logger.Info("Creating bootstrap data location", "location", naming.RootfsEnvVarsFile)

	if err := remoteClient.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s'", filepath.Dir(naming.RootfsEnvVarsFile)))); err != nil {
		b.logger.Error("BOOTSTRAP INCOMPLETE: Failed creating bootstrap environment directory",
			"directory", filepath.Dir(naming.RootfsEnvVarsFile),
			"reason", err,
			"service-env", serviceEnv)
		return err
	}

	b.logger.Info("Uploading the service environment file")

	serviceEnvReader := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(strings.Join(serviceEnv, "\n") + "\n"))), nil
	}

	if err := remoteClient.PutResource(resources.
		NewResolvedFileResource(serviceEnvReader,
			fs.FileMode(0644),
			"",
			naming.RootfsEnvVarsFile,
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

		serviceInstallerReader := func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader([]byte(installerBytes))), nil
		}

		if err := remoteClient.RunCommand(commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s'", filepath.Dir(naming.ServiceInstallerFile)))); err != nil {
			b.logger.Error("BOOTSTRAP INCOMPLETE: Failed creating service installer directory",
				"directory", filepath.Dir(naming.ServiceInstallerFile),
				"reason", err)
			return err
		}

		// upload the installer:
		if err := remoteClient.PutResource(resources.
			NewResolvedFileResource(serviceInstallerReader,
				fs.FileMode(0754),
				"",
				naming.ServiceInstallerFile,
				commands.DefaultWorkdir(),
				commands.DefaultUser())); err != nil {
			b.logger.Error("Failed uploading the service environment file",
				"reason", err,
				"service-env", serviceEnv)
			return err
		}

		b.logger.Info("Executing service installer file")

		// execute the installer, we leave it there...:
		if err := remoteClient.RunCommand(commands.RunWithDefaults(naming.ServiceInstallerFile)); err != nil {
			b.logger.Error("BOOTSTRAP INCOMPLETE: Failed executing the local service installer",
				"installer-path", naming.ServiceInstallerFile,
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

func (b *defaultBuild) AddInstructions(instructions ...interface{}) error {
	for _, input := range instructions {
		switch tinput := input.(type) {
		case commands.Add:
			tinput.User = b.currentUser
			tinput.Workdir = b.currentWorkdir
			b.instructions = append(b.instructions, tinput)
		case commands.Arg:
			argValue, hadValue := tinput.Value()
			if buildArgValue, ok := b.buildArgs[tinput.Key()]; ok {
				argValue = buildArgValue
			} else {
				if !hadValue {
					return fmt.Errorf("build arg %q: no value", tinput.Key())
				}
			}
			key, value := b.buildEnv.Put(tinput.Key(), argValue)
			b.currentArgs[key] = value
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
			key, value := b.buildEnv.Put(tinput.Name, tinput.Value)
			b.currentEnv[key] = value
		case commands.Expose:
			b.exposedPorts = append(b.exposedPorts, tinput.RawValue)
		case commands.From:
			b.isDependencyBuild = tinput.StageName != ""
			b.from = tinput
		case commands.Label:
			b.currentMetadata[tinput.Key] = b.buildEnv.Expand(tinput.Value)
		case commands.Run:
			tinput.Args = b.currentArgs
			tinput.Env = b.currentEnv
			tinput.Shell = b.currentShell
			tinput.User = b.currentUser
			tinput.Workdir = b.currentWorkdir
			tinput.Command = b.buildEnv.Expand(tinput.Command)
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
			if strings.HasPrefix(tinput.Value, "/") {
				b.currentWorkdir = tinput
			} else {
				b.currentWorkdir.Value = filepath.Join(b.currentWorkdir.Value, tinput.Value)
			}
		}
	}

	return nil
}

func (b *defaultBuild) ExposedPorts() []string {
	return b.exposedPorts
}

func (b *defaultBuild) From() commands.From {
	return b.from
}

func (b *defaultBuild) Metadata() map[string]string {
	return b.currentMetadata
}

func (b *defaultBuild) WithBuildArgs(input map[string]string) Build {
	b.buildArgs = input
	return b
}

func (b *defaultBuild) WithDependencyResources(input map[string][]resources.ResolvedResource) Build {
	b.dependentResources = input
	return b
}

func (b *defaultBuild) WithExcludes(input []string) Build {
	b.excludes = input
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
		buildEnv:          env.NewBuildEnv(),
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
		resolvedResources: map[string][]resources.ResolvedResource{},
	}
}

func mapShellString(input []string) []string {
	for idx, item := range input {
		input[idx] = strings.ReplaceAll(item, "\"", "\"\"")
	}
	return input
}
