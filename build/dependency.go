package build

import (
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/build/commands"
	"github.com/combust-labs/firebuild/build/resources"
	"github.com/combust-labs/firebuild/build/stage"
	"github.com/combust-labs/firebuild/build/utils"
	"github.com/combust-labs/firebuild/containers"
	"github.com/hashicorp/go-hclog"
)

// DependencyBuild represents the build process of the main FS dependency.
// This builds a stage using Docker and extracts the required contents from
// the build.
type DependencyBuild interface {
	Build([]commands.Copy) ([]resources.ResolvedResource, error)
	WithLogger(hclog.Logger) DependencyBuild
	getDependencyDockerfileContent() []string
}

type defaultDependencyBuild struct {
	contextDirectory string
	logger           hclog.Logger
	stage            stage.Stage
	tempDir          string
}

// NewDefaultDependencyBuild creates a new dependency builder using the default implementation.
func NewDefaultDependencyBuild(st stage.Stage, tempDir, contextDir string) DependencyBuild {
	return &defaultDependencyBuild{
		contextDirectory: contextDir,
		logger:           hclog.Default(),
		stage:            st,
		tempDir:          tempDir,
	}
}

func (ddb *defaultDependencyBuild) Build(externalCopies []commands.Copy) ([]resources.ResolvedResource, error) {

	emptyResponse := []resources.ResolvedResource{}

	client, clientErr := containers.GetDefaultClient()
	if clientErr != nil {
		return emptyResponse, fmt.Errorf("error fetching Docker client: %+v", clientErr)
	}

	// TODO: verify that this is actually possible with Docker.
	// Do not return early, maybe somebody attempts to build a base image
	// using the multistage build but without extracting actual resources.

	// The cloned sources reside in .../sources directory, let's write our stage Dockerfile in there
	randFileName := strings.ToLower(utils.RandStringBytes(32))
	stageDockerfile := filepath.Join(ddb.contextDirectory, randFileName)
	fullTagName := fmt.Sprintf("%s:build", randFileName)

	if err := ioutil.WriteFile(stageDockerfile, []byte(strings.Join(ddb.getDependencyDockerfileContent(), "\n")), fs.ModePerm); err != nil {
		return emptyResponse, fmt.Errorf("Failed writing stage Dockerfile: %+v", err)
	}

	if buildError := containers.ImageBuild(context.Background(), client, ddb.logger,
		ddb.contextDirectory, randFileName, fullTagName); buildError != nil {
		return emptyResponse, fmt.Errorf("Failed building stage Docker image: %+v", buildError)
	}

	defer func() {
		if removeError := containers.ImageRemove(context.Background(), client, ddb.logger, fullTagName); removeError != nil {
			ddb.logger.Error("Failed deleting stage Docker image", "reason", removeError)
		}
	}()

	exportsRoot := filepath.Join(ddb.tempDir, fmt.Sprintf("%s-export", ddb.stage.Name()))

	resolvedResources, exportErr := containers.ImageExportStageDependentResources(context.Background(),
		client, ddb.logger, ddb.stage, exportsRoot, externalCopies, fullTagName)
	if exportErr != nil {
		return emptyResponse, fmt.Errorf("Failed exporting prefixes from the image: %+v", exportErr)
	}

	return resolvedResources, nil
}

func (ddb *defaultDependencyBuild) WithLogger(input hclog.Logger) DependencyBuild {
	ddb.logger = input
	return ddb
}

// This function converts commands for a given stage back to the Dockerfile format
// but removes `as ...` from the FROM command.
func (ddb *defaultDependencyBuild) getDependencyDockerfileContent() []string {
	stringCommands := []string{}
	for _, cmd := range ddb.stage.Commands() {
		switch tcmd := cmd.(type) {
		case commands.From:
			if tcmd.StageName != "" {
				tcmd.OriginalCommand = fmt.Sprintf("FROM %s", tcmd.BaseImage)
				cmd = tcmd
			}
		}
		if casted, ok := cmd.(commands.DockerfileSerializable); ok {
			found := false
			originalCommand := casted.GetOriginal()
			for _, strCmd := range stringCommands {
				if strCmd == originalCommand {
					found = true
					break
				}
			}
			if !found {
				stringCommands = append(stringCommands, originalCommand)
			}
		}
	}
	return stringCommands
}
