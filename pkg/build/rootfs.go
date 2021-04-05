package build

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild-shared/build/commands"
	"github.com/combust-labs/firebuild-shared/build/resources"
	"github.com/combust-labs/firebuild-shared/build/rootfs"
	"github.com/combust-labs/firebuild-shared/env"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/hashicorp/go-hclog"
)

// Build represents the build operation.
type Build interface {
	AddInstructions(...interface{}) error
	CreateContext(rootfs.Resources) (*rootfs.WorkContext, error)
	EntrypointInfo() *EntrypointInfo
	ExposedPorts() []string
	From() commands.From
	Metadata() map[string]string
	Volumes() []string
	WithBuildArgs(map[string]string) Build
	WithExcludes([]string) Build
	WithLogger(hclog.Logger) Build
	WithPostBuildCommands(...commands.Run) Build
	WithPreBuildCommands(...commands.Run) Build
	WithResolver(resources.Resolver) Build
}

type defaultBuild struct {
	buildArgs         map[string]string
	buildEnv          env.BuildEnv
	currentArgs       map[string]string
	currentCmd        commands.Cmd
	currentEntrypoint commands.Entrypoint
	currentEnv        map[string]string
	currentMetadata   map[string]string
	currentShell      commands.Shell
	currentUser       commands.User
	currentWorkdir    commands.Workdir

	exposedPorts      []string
	excludes          []string
	from              commands.From
	instructions      []interface{}
	isDependencyBuild bool
	logger            hclog.Logger
	resolver          resources.Resolver

	postBuildCommands []commands.Run
	preBuildCommands  []commands.Run

	volumes []string
}

func (b *defaultBuild) CreateContext(dependencies rootfs.Resources) (*rootfs.WorkContext, error) {

	ctx := &rootfs.WorkContext{
		ExecutableCommands: []commands.VMInitSerializableCommand{},
		ResourcesResolved:  make(rootfs.Resources),
	}

	patternMatcher, matcherCreateErr := fileutils.NewPatternMatcher(b.excludes)
	if matcherCreateErr != nil {
		b.logger.Error("failed creating excludes pattern matcher", "reason", matcherCreateErr)
		return nil, matcherCreateErr
	}

	b.logger.Info("building from", "base", b.from.BaseImage)

	// validate resources first:
	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case commands.Add:
			resolvedResource, err := b.resolver.ResolveAdd(tcommand)
			if err != nil {
				b.logger.Error("failed resolving ADD resource", "reason", err)
				return nil, err
			}
			ctx.ResourcesResolved[tcommand.Source] = resolvedResource
		case commands.Copy:
			resolvedResource, err := b.resolver.ResolveCopy(tcommand)
			if err != nil {
				b.logger.Error("failed resolving COPY resource", "reason", err)
				return nil, err
			}
			ctx.ResourcesResolved[tcommand.Source] = resolvedResource
		}
	}

	for _, cmd := range b.preBuildCommands {
		ctx.ExecutableCommands = append(ctx.ExecutableCommands, cmd)
	}

	patternMatcherFunc := func(input string) bool {
		pathMatched, matchErr := patternMatcher.Matches(input)
		if matchErr != nil {
			b.logger.Warn("error while matching path for PutResource ADD/COPY, skipping", "source", input, "reason", matchErr)
			return false // skip this resource
		}
		if !pathMatched {
			// not matched by exclusions, is to be included
			return false
		}
		return true
	}

	for _, command := range b.instructions {
		switch tcommand := command.(type) {
		case commands.Add:
			if patternMatcherFunc(tcommand.Source) {
				b.logger.Debug("skipping excluded path for PutResource ADD", "source", tcommand.Source)
				continue
			}
			if _ /* resource */, ok := ctx.ResourcesResolved[tcommand.Source]; ok {
				b.logger.Info("Putting ADD resource", "source", tcommand.Source)
				ctx.ExecutableCommands = append(ctx.ExecutableCommands, tcommand)
			} else {
				b.logger.Error("ADD resource required but not resolved", "source", tcommand.Source)
			}
		case commands.Copy:
			if patternMatcherFunc(tcommand.Source) {
				b.logger.Debug("skipping excluded path for PutResource COPY", "source", tcommand.Source)
				continue
			}
			// dependency resources exist for COPY commands only:
			if tcommand.Stage != "" {
				// we need to locate a dependency resource
				dependencyResources, ok := dependencies[tcommand.Stage]
				if !ok {
					b.logger.Error("PutResource COPY resource failed, no dependency resource stage", "source", tcommand.Source, "stage", tcommand.Stage)
					return nil, fmt.Errorf("no dependency stage %s", tcommand.Stage)
				}
				resourceWasProcessed := false
				for _, dependencyResource := range dependencyResources {
					if strings.HasPrefix(dependencyResource.SourcePath(), tcommand.Source) {

						b.logger.Info("Putting COPY resource from dependency", "source", tcommand.Source, "stage", tcommand.Stage)

						sourcePath := fmt.Sprintf("%s://%s", tcommand.Stage, dependencyResource.SourcePath())

						ctx.ResourcesResolved[sourcePath] = []resources.ResolvedResource{dependencyResource}

						ctx.ExecutableCommands = append(ctx.ExecutableCommands, commands.Copy{
							OriginalCommand:    tcommand.OriginalCommand,
							OriginalSource:     dependencyResource.ResolvedURIOrPath(),
							Source:             sourcePath,
							Target:             dependencyResource.TargetPath(),
							Workdir:            tcommand.Workdir,
							Stage:              tcommand.Stage,
							User:               tcommand.User,
							UserFromLocalChown: tcommand.UserFromLocalChown,
						})

						resourceWasProcessed = true
					}
				}
				if !resourceWasProcessed {
					b.logger.Error("COPY resource required from stage but not resolved", "source", tcommand.Source, "stage", tcommand.Stage)
				}
				continue
			}
			if _ /* resource */, ok := ctx.ResourcesResolved[tcommand.Source]; ok {
				b.logger.Info("Putting COPY resource", "source", tcommand.Source)
				ctx.ExecutableCommands = append(ctx.ExecutableCommands, tcommand)
			} else {
				b.logger.Error("COPY resource required but not resolved", "source", tcommand.Source)
			}

		case commands.Run:
			ctx.ExecutableCommands = append(ctx.ExecutableCommands, tcommand)
		case commands.Volume:
			for _, vol := range tcommand.Values {
				b.volumes = append(b.volumes, vol)
				run := commands.RunWithDefaults(fmt.Sprintf("mkdir -p '%s'", vol))
				run.User = tcommand.User
				run.Workdir = tcommand.Workdir
				ctx.ExecutableCommands = append(ctx.ExecutableCommands, run)
			}
		}
	}

	for _, cmd := range b.postBuildCommands {
		ctx.ExecutableCommands = append(ctx.ExecutableCommands, cmd)
	}

	return ctx, nil
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

func (b *defaultBuild) EntrypointInfo() *EntrypointInfo {
	return &EntrypointInfo{
		Cmd:        b.currentCmd,
		Entrypoint: b.currentEntrypoint,
	}
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

func (b *defaultBuild) Volumes() []string {
	return b.volumes
}

func (b *defaultBuild) WithBuildArgs(input map[string]string) Build {
	b.buildArgs = input
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
		volumes:           []string{},
	}
}

func mapShellString(input []string) []string {
	for idx, item := range input {
		input[idx] = strings.ReplaceAll(item, "\"", "\"\"")
	}
	return input
}
