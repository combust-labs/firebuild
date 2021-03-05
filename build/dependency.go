package build

import (
	tar "archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/combust-labs/firebuild/build/commands"
	"github.com/combust-labs/firebuild/build/resources"
	"github.com/combust-labs/firebuild/build/stage"
	"github.com/combust-labs/firebuild/build/utils"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/strslice"
	docker "github.com/docker/docker/client"
	dockerArchive "github.com/docker/docker/pkg/archive"
	"github.com/hashicorp/go-hclog"
)

// DependencyBuild represents the build process of the main FS dependency.
// This builds a stage using Docker and extracts the required contents from
// the build.
type DependencyBuild interface {
	Build([]commands.Copy) ([]resources.ResolvedResource, error)
	Cleanup()
	FindImageIDByTag(string) (string, error)
	ImageBaseOSExport(path, tagName string) error
	ImageBuild(source, dockerfilePath, tagName string) error
	ImageRemove(tagName string) error
	WithLogger(hclog.Logger) DependencyBuild
	getDependencyDockerfileContent() []string
	imageExport(exportsRoot string, externalCopies []commands.Copy, tagName string) ([]resources.ResolvedResource, error)
}

type defaultDependencyBuild struct {
	cleanupFuncs     []func()
	contextDirectory string
	dockerClient     *docker.Client
	logger           hclog.Logger
	stage            stage.Stage
	tempDir          string
}

// NewDefaultDependencyBuild creates a new dependency builder using the default implementation.
func NewDefaultDependencyBuild(st stage.Stage, tempDir, contextDir string) (DependencyBuild, error) {
	// get a Docker client:
	dockerEnvClient, err := docker.NewEnvClient()
	if err != nil {
		return nil, err
	}
	return &defaultDependencyBuild{
		cleanupFuncs: []func(){
			func() {
				dockerEnvClient.Close()
			},
		},
		contextDirectory: contextDir,
		dockerClient:     dockerEnvClient,
		logger:           hclog.Default(),
		stage:            st,
		tempDir:          tempDir,
	}, nil
}

func (ddb *defaultDependencyBuild) Build(externalCopies []commands.Copy) ([]resources.ResolvedResource, error) {

	// TODO: verify that this is actually possible with Docker.
	// Do not return early, maybe somebody attempts to build a base image
	// using the multistage build but without extracting actual resources.

	// The cloned sources reside in .../sources directory, let's write our stage Dockerfile in there
	randFileName := strings.ToLower(utils.RandStringBytes(32))
	stageDockerfile := filepath.Join(ddb.contextDirectory, randFileName)
	fullTagName := fmt.Sprintf("%s:build", randFileName)

	emptyResponse := []resources.ResolvedResource{}

	if err := ioutil.WriteFile(stageDockerfile, []byte(strings.Join(ddb.getDependencyDockerfileContent(), "\n")), fs.ModePerm); err != nil {
		return emptyResponse, fmt.Errorf("Failed writing stage Dockerfile: %+v", err)
	}

	if buildError := ddb.ImageBuild(ddb.contextDirectory, randFileName, fullTagName); buildError != nil {
		return emptyResponse, fmt.Errorf("Failed building stage Docker image: %+v", buildError)
	}

	defer func() {
		if removeError := ddb.ImageRemove(fullTagName); removeError != nil {
			ddb.logger.Error("Failed deleting stage Docker image", "reason", removeError)
		}
	}()

	exportsRoot := filepath.Join(ddb.tempDir, fmt.Sprintf("%s-export", ddb.stage.Name()))

	resolvedResources, exportErr := ddb.imageExport(exportsRoot, externalCopies, fullTagName)
	if exportErr != nil {
		return emptyResponse, fmt.Errorf("Failed exporting prefixes from the image: %+v", exportErr)
	}

	return resolvedResources, nil
}

func (ddb *defaultDependencyBuild) Cleanup() {
	for _, f := range ddb.cleanupFuncs {
		f()
	}
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

func (ddb *defaultDependencyBuild) ImageBuild(source, dockerfilePath, tagName string) error {

	if !strings.HasSuffix(source, "/") {
		source = fmt.Sprintf("%s/", source)
	}

	opLogger := ddb.logger.With("dir-context", source, "dockerfile", dockerfilePath, "tag-name", tagName)

	// convert the context into a tar:
	tar, err := dockerArchive.TarWithOptions(source, &dockerArchive.TarOptions{})
	if err != nil {
		opLogger.Error("failed creating tar archive as Docker build context", "reason", err)
		return err
	}
	defer tar.Close()

	// build the image:
	buildResponse, buildErr := ddb.dockerClient.ImageBuild(context.Background(), tar, types.ImageBuildOptions{
		Dockerfile:  dockerfilePath,
		Tags:        []string{tagName},
		ForceRemove: true,
		Remove:      true,
	})
	if buildErr != nil {
		opLogger.Error("failed creating Docker image", "reason", buildErr)
		return buildErr
	}

	// read output:
	scanner := bufio.NewScanner(buildResponse.Body)
	lastLine := ""
	for scanner.Scan() {
		lastLine := scanner.Text()
		out := &dockerOutStream{}
		if err := json.Unmarshal([]byte(lastLine), out); err != nil {
			opLogger.Warn("Docker build output not a stream line, skipping", "reason", err)
			continue
		}
		opLogger.Info("docker image build status", "stream", strings.TrimSpace(out.Stream))
	}

	// deal with failed builds:
	errLine := &dockerErrorLine{}
	json.Unmarshal([]byte(lastLine), errLine)
	if errLine.Error != "" {
		opLogger.Error("Docker image build finished with error", "reason", errLine.Error)
		return fmt.Errorf(errLine.Error)
	}

	if scannerErr := scanner.Err(); scannerErr != nil {
		opLogger.Error("Docker response scanner finished with error", "reason", scannerErr)
		return scannerErr
	}

	// all okay:
	return nil
}

func (ddb *defaultDependencyBuild) ImageBaseOSExport(path, tagName string) error {

	opLogger := ddb.logger.With("tag-name", tagName)

	// TODO: extract to the outside of the function
	mkdirOnlyDirs := []string{"/boot", "/opt", "/proc", "/run", "/srv", "/sys", "/tmp", "/var"}
	containerStopTimeout := time.Duration(time.Second * 15)
	fsCopyTimeout := time.Duration(time.Second * 15)

	containerConfig := &container.Config{
		OpenStdin: true,
		Tty:       true,
		Cmd:       strslice.StrSlice([]string{"/bin/sh"}), // TODO: extract this to the outside of the function
		Image:     tagName,
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: path,
				Target: "/export-rootfs", // TODO: extract this to the outside of the function
			},
		},
	}

	// Doing this export through TAR, without starting the container, is much more work
	// than simply starting a container. When doing this by doing image export,
	// there is a need to:
	// - track links manually
	// - track symlinks manually
	// - all the chmod, chown, chtimes accounting
	// - special devices aren't properly carried through

	opLogger.Debug("starting base OS Docker container for rootfs export")

	containerStartResponse, startErr := ddb.dockerClient.ContainerCreate(context.Background(), containerConfig, hostConfig, nil, nil, "")
	if startErr != nil {
		opLogger.Error("failed creating a Docker container", "reason", startErr)
		return startErr
	}

	opLogger = opLogger.With("container-id", containerStartResponse.ID)
	opLogger.Debug("container started")

	if err := ddb.dockerClient.ContainerStart(context.Background(), containerStartResponse.ID, types.ContainerStartOptions{}); err != nil {
		opLogger.Error("failed starting a Docker container", "reason", err)
		return err
	}

	mkdirOnlyDirsStr := "LIST=\"" + strings.Join(mkdirOnlyDirs, " ") + "\"; "

	commands := []string{
		// these ones should have an empty directory only:
		mkdirOnlyDirsStr + "for d in $(find / -maxdepth 1 -type d); do if echo $LIST | grep -w $d > /dev/null; then mkdir /export-rootfs${d}; fi; done; exit 0",
		// these are the ones I want to copy, when they don't exist in the list:
		mkdirOnlyDirsStr + "for d in $(find / -maxdepth 1 -type d); do if echo $LIST | grep -v -w $d > /dev/null; then case \"$d\" in \"/\") ;; *) tar c \"$d\" | tar x -C /export-rootfs ;; esac; fi; done",
	}

	for idx, command := range commands {

		opLogger.Debug(fmt.Sprintf("running exec %d of %d", idx+1, len(commands)))

		execIDResponse, execErr := ddb.dockerClient.ContainerExecCreate(context.Background(), containerStartResponse.ID, types.ExecConfig{
			AttachStdout: true,
			AttachStderr: true,
			Cmd:          []string{"/bin/sh", "-c", command}, // TODO: extract the shell to the outside of the function
		})
		if execErr != nil {
			opLogger.Error("error creating exec", "container-id", containerStartResponse.ID, "reason", execErr)
			return execErr
		}

		hijackedConn, execAttachErr := ddb.dockerClient.ContainerExecAttach(context.Background(), execIDResponse.ID, types.ExecStartCheck{
			Tty: true,
		})
		if execAttachErr != nil {
			opLogger.Error("error attaching exec", "reason", execAttachErr)
			return execAttachErr
		}

		chanDone := make(chan struct{}, 1)
		chanError := make(chan error, 1)
		execReadCtx, execReadCtxCancelFunc := context.WithTimeout(context.Background(), fsCopyTimeout)
		defer execReadCtxCancelFunc()

		go func() {
			defer hijackedConn.Close()
			for {
				bs, err := hijackedConn.Reader.ReadBytes('\n')
				if execReadCtx.Err() != nil {
					return
				}
				if err != nil {
					if err == io.EOF {
						close(chanDone)
						return // finished reading successfully
					}
					chanError <- err
					return
				}
				opLogger.Debug("exec attach output", strings.TrimSpace(string(bs)))
			}
		}()

		select {
		case <-chanDone:
			opLogger.Debug(fmt.Sprintf("exec %d of %d finished successfully", idx+1, len(commands)))
			close(chanError)
		case execReadErr := <-chanError:
			opLogger.Error(fmt.Sprintf("exec %d of %d finished with error", idx+1, len(commands)), "reason", execReadErr)
			close(chanDone)
			return execReadErr
		case <-execReadCtx.Done():
			// the context finished with error
			close(chanDone)
			close(chanError)
			if execReadCtx.Err() != nil {
				opLogger.Error(fmt.Sprintf("exec %d of %d finished with context error", idx+1, len(commands)), "reason", execReadCtx.Err())
				return execReadCtx.Err()
			}
		}
	}

	opLogger.Debug("stopping container")

	go func() {
		if stopError := ddb.dockerClient.ContainerStop(context.Background(), containerStartResponse.ID, &containerStopTimeout); stopError != nil {
			opLogger.Warn("problem stopping the container gracefully, killing", "reason", stopError)
			if killError := ddb.dockerClient.ContainerKill(context.Background(), containerStartResponse.ID, "SIGKILL"); killError != nil {
				opLogger.Warn("container kill also returned an error", "reason", killError)
			}
		}
	}()

	opLogger.Debug("waiting for container to stop")

	chanStopOK, chanStopErr := ddb.dockerClient.ContainerWait(context.Background(), containerStartResponse.ID, container.WaitConditionNotRunning)
	select {
	case ok := <-chanStopOK:
		opLogger.Debug("container stopped", "exit-code", ok.StatusCode, "error-message", ok.Error)
	case stopErr := <-chanStopErr:
		opLogger.Warn("container stop wait returned an error", "reason", stopErr)
	}

	containerRemoveOptions := types.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	}

	opLogger.Debug("container started")

	go func() {
		if removeError := ddb.dockerClient.ContainerRemove(context.Background(), containerStartResponse.ID, containerRemoveOptions); removeError != nil {
			opLogger.Warn("problem removing the container", "reason", removeError)
		}
	}()

	opLogger.Debug("waiting for container to be removed")

	chanRemoveOK, chanRemoveErr := ddb.dockerClient.ContainerWait(context.Background(), containerStartResponse.ID, container.WaitConditionRemoved)
	select {
	case ok := <-chanRemoveOK:
		opLogger.Debug("container removed", "exit-code", ok.StatusCode, "error-message", ok.Error)
	case removeError := <-chanRemoveErr:
		opLogger.Warn("container stop wait returned an error", "reason", removeError)
	}

	return nil
}

/*
Function extracts prefixes from the Docker image.
The tar file exported from Docker contains a directory for every layer.
Each layer directory contains 3 files: VERSION, json and layer.tar
Complete example:
-------------------------------------------------------------------------------------------------------------------------
drwxr-xr-x 0/0               0 2021-03-02 20:05 0b7ef4d8f09fb5aef4ee6cf368d0a76dc915a5e1292249d21b46c7a10740bfb5/
-rw-r--r-- 0/0               3 2021-03-02 20:05 0b7ef4d8f09fb5aef4ee6cf368d0a76dc915a5e1292249d21b46c7a10740bfb5/VERSION
-rw-r--r-- 0/0             477 2021-03-02 20:05 0b7ef4d8f09fb5aef4ee6cf368d0a76dc915a5e1292249d21b46c7a10740bfb5/json
-rw-r--r-- 0/0            2560 2021-03-02 20:05 0b7ef4d8f09fb5aef4ee6cf368d0a76dc915a5e1292249d21b46c7a10740bfb5/layer.tar
drwxr-xr-x 0/0               0 2021-03-02 20:05 214907aec1a34d707f3f25123e2b52ffe174c784e9dd9feb631ce0025977b065/
-rw-r--r-- 0/0               3 2021-03-02 20:05 214907aec1a34d707f3f25123e2b52ffe174c784e9dd9feb631ce0025977b065/VERSION
-rw-r--r-- 0/0             477 2021-03-02 20:05 214907aec1a34d707f3f25123e2b52ffe174c784e9dd9feb631ce0025977b065/json
-rw-r--r-- 0/0       300947456 2021-03-02 20:05 214907aec1a34d707f3f25123e2b52ffe174c784e9dd9feb631ce0025977b065/layer.tar
-rw-r--r-- 0/0            6542 2021-03-02 20:05 24eca7c8603300acb80a237c13236d546ab7e91e25fab5638685b266c1e319ea.json
drwxr-xr-x 0/0               0 2021-03-02 20:05 5686858b60db198d4cf472e7f07253186b904981edfe5208d2fcdaa32dbf1ee4/
-rw-r--r-- 0/0               3 2021-03-02 20:05 5686858b60db198d4cf472e7f07253186b904981edfe5208d2fcdaa32dbf1ee4/VERSION
-rw-r--r-- 0/0             477 2021-03-02 20:05 5686858b60db198d4cf472e7f07253186b904981edfe5208d2fcdaa32dbf1ee4/json
-rw-r--r-- 0/0            4096 2021-03-02 20:05 5686858b60db198d4cf472e7f07253186b904981edfe5208d2fcdaa32dbf1ee4/layer.tar
drwxr-xr-x 0/0               0 2021-03-02 20:05 58069b027e284e9a256e49a74c1b60e4f3ac22d51794ca92f3e24706d90858a7/
-rw-r--r-- 0/0               3 2021-03-02 20:05 58069b027e284e9a256e49a74c1b60e4f3ac22d51794ca92f3e24706d90858a7/VERSION
-rw-r--r-- 0/0             477 2021-03-02 20:05 58069b027e284e9a256e49a74c1b60e4f3ac22d51794ca92f3e24706d90858a7/json
-rw-r--r-- 0/0       226923520 2021-03-02 20:05 58069b027e284e9a256e49a74c1b60e4f3ac22d51794ca92f3e24706d90858a7/layer.tar
drwxr-xr-x 0/0               0 2021-03-02 20:05 647a40d74ab065cb5db4efe59470469cd1ee3ac219e23c2c69a66a7d55e2e548/
-rw-r--r-- 0/0               3 2021-03-02 20:05 647a40d74ab065cb5db4efe59470469cd1ee3ac219e23c2c69a66a7d55e2e548/VERSION
-rw-r--r-- 0/0             477 2021-03-02 20:05 647a40d74ab065cb5db4efe59470469cd1ee3ac219e23c2c69a66a7d55e2e548/json
-rw-r--r-- 0/0            2560 2021-03-02 20:05 647a40d74ab065cb5db4efe59470469cd1ee3ac219e23c2c69a66a7d55e2e548/layer.tar
drwxr-xr-x 0/0               0 2021-03-02 20:05 8196e5d2961481647c0faf63ce4c3518f4098df756e79c1fa7b496d902a6c67e/
-rw-r--r-- 0/0               3 2021-03-02 20:05 8196e5d2961481647c0faf63ce4c3518f4098df756e79c1fa7b496d902a6c67e/VERSION
-rw-r--r-- 0/0             477 2021-03-02 20:05 8196e5d2961481647c0faf63ce4c3518f4098df756e79c1fa7b496d902a6c67e/json
-rw-r--r-- 0/0        22520832 2021-03-02 20:05 8196e5d2961481647c0faf63ce4c3518f4098df756e79c1fa7b496d902a6c67e/layer.tar
drwxr-xr-x 0/0               0 2021-03-02 20:05 896d1cab2467fe9cc0281301e206c79c340991a03f36f5177d88c07d7b0d3592/
-rw-r--r-- 0/0               3 2021-03-02 20:05 896d1cab2467fe9cc0281301e206c79c340991a03f36f5177d88c07d7b0d3592/VERSION
-rw-r--r-- 0/0            1502 2021-03-02 20:05 896d1cab2467fe9cc0281301e206c79c340991a03f36f5177d88c07d7b0d3592/json
-rw-r--r-- 0/0        79211008 2021-03-02 20:05 896d1cab2467fe9cc0281301e206c79c340991a03f36f5177d88c07d7b0d3592/layer.tar
drwxr-xr-x 0/0               0 2021-03-02 20:05 b81e7efb7df998cf16f6597240c71007836ccef427880030fec1abb033cd7ddd/
-rw-r--r-- 0/0               3 2021-03-02 20:05 b81e7efb7df998cf16f6597240c71007836ccef427880030fec1abb033cd7ddd/VERSION
-rw-r--r-- 0/0             477 2021-03-02 20:05 b81e7efb7df998cf16f6597240c71007836ccef427880030fec1abb033cd7ddd/json
-rw-r--r-- 0/0          760832 2021-03-02 20:05 b81e7efb7df998cf16f6597240c71007836ccef427880030fec1abb033cd7ddd/layer.tar
drwxr-xr-x 0/0               0 2021-03-02 20:05 c7987d414b44d6c3a888b44a81ad6d9e52c09f482b95cb61ead8e575bb2b0a7f/
-rw-r--r-- 0/0               3 2021-03-02 20:05 c7987d414b44d6c3a888b44a81ad6d9e52c09f482b95cb61ead8e575bb2b0a7f/VERSION
-rw-r--r-- 0/0             401 2021-03-02 20:05 c7987d414b44d6c3a888b44a81ad6d9e52c09f482b95cb61ead8e575bb2b0a7f/json
-rw-r--r-- 0/0         5847552 2021-03-02 20:05 c7987d414b44d6c3a888b44a81ad6d9e52c09f482b95cb61ead8e575bb2b0a7f/layer.tar
-------------------------------------------------------------------------------------------------------------------------
*/
func (ddb *defaultDependencyBuild) imageExport(exportsRoot string, externalCopies []commands.Copy, tagName string) ([]resources.ResolvedResource, error) {

	opLogger := ddb.logger.With("exports-root", exportsRoot, "tag-name", tagName)

	resolvedResources := []resources.ResolvedResource{}
	opCopies := []commands.Copy{}

	for _, externalCopy := range externalCopies {
		if externalCopy.Stage != ddb.stage.Name() {
			continue
		}
		opCopies = append(opCopies, externalCopy)
	}

	if len(opCopies) == 0 {
		return resolvedResources, nil // shortcircuit, nothing to look up
	}
	opLogger.Debug("exporting Docker stage build image")
	imageID, err := ddb.FindImageIDByTag(tagName)
	if err != nil {
		opLogger.Error("failed fetching Docker image ID by tag", "reason", err)
		return resolvedResources, err
	}

	opLogger = opLogger.With("image-id", imageID)

	// Make sure the owning directory exists:
	opLogger.Debug("ensuring the exports root directory exists")
	if err := os.MkdirAll(exportsRoot, fs.ModePerm); err != nil {
		opLogger.Error("failed creating exports root directory on disk", "reason", err)
		return resolvedResources, err
	}

	reader, err := ddb.dockerClient.ImageSave(context.Background(), []string{imageID})
	if err != nil {
		opLogger.Error("failed creating io.Reader for image save", "reason", err)
		return resolvedResources, err
	}
	defer reader.Close()

	opLogger.Debug("reading Docker image data")

	dockerFsReader := tar.NewReader(reader)

	for {

		dockerFsHeader, dockerFsError := dockerFsReader.Next()
		if dockerFsError != nil {
			if dockerFsError == io.EOF {
				break
			}
			opLogger.Error("error while reading exported Docker file system", "reason", dockerFsError)
			return resolvedResources, dockerFsError
		}

		// Only intersted in layer tars...
		if strings.HasSuffix(dockerFsHeader.Name, "/layer.tar") {

			opLogger.Debug("processing layer", "layer", dockerFsHeader.Name)

			layerReader := tar.NewReader(dockerFsReader)

			for {

				layerHeader, layerHeaderErr := layerReader.Next()
				if layerHeaderErr != nil {
					if layerHeaderErr == io.EOF {
						break
					}
					opLogger.Error("error while reading layer file system", "reason", layerReader)
					return resolvedResources, layerHeaderErr
				}

				for _, opCopy := range opCopies {
					// files in the layer.tar to not have the leading /
					if strings.HasPrefix("/"+layerHeader.Name, opCopy.Source) {
						if !layerHeader.FileInfo().IsDir() {
							// gotta read the file...
							opLogger.Debug("reading file", "layer", layerHeader.Name, "matched-prefix", opCopy.Source)
							targetPath := filepath.Join(exportsRoot, layerHeader.Name)
							// make sure we have the parent directory for the target:
							if parentDirErr := os.MkdirAll(filepath.Dir(targetPath), fs.ModePerm); err != nil {
								opLogger.Error("failed creating directories for the layer tar entry in exports root",
									"layer", layerHeader.Name,
									"matched-prefix", opCopy.Source,
									"reason", parentDirErr)
								return resolvedResources, parentDirErr
							}
							// create a file for what we have to read out:
							opLogger.Debug("creating target file for entry extraction", "layer", layerHeader.Name, "matched-prefix", opCopy.Source)
							targetFile, fileCreateErr := os.Create(targetPath)
							if fileCreateErr != nil {
								opLogger.Error("failed creating target file to extracted entry",
									"layer", layerHeader.Name,
									"matched-prefix", opCopy.Source,
									"reason", fileCreateErr)
								return resolvedResources, fileCreateErr
							}
							opLogger.Debug("reading target file contents",
								"layer", layerHeader.Name,
								"matched-prefix", opCopy.Source,
								"target-file", targetFile.Name())
							targetBuf := make([]byte, 8*1024*1024)
							for {
								read, e := layerReader.Read(targetBuf)
								if read == 0 && e == io.EOF {
									break
								}
								// write chunk to file:
								targetFile.Write(targetBuf[0:read])
							}
							targetFile.Close()
							opLogger.Debug("target file contents read",
								"layer", layerHeader.Name,
								"matched-prefix", opCopy.Source,
								"target-file", targetFile.Name())
							if chmodErr := os.Chmod(targetFile.Name(), layerHeader.FileInfo().Mode().Perm()); err != nil {
								opLogger.Error("failed chaning target file mode",
									"layer", layerHeader.Name,
									"matched-prefix", opCopy.Source,
									"target-file", targetFile.Name(),
									"reason", chmodErr)
								return resolvedResources, chmodErr
							}

							resourceFilePath := targetFile.Name()
							// here, we have the vanilla resource we are looking for:
							resourceReader := func() (io.ReadCloser, error) {
								file, err := os.Open(resourceFilePath)
								if err != nil {
									return nil, fmt.Errorf("dependent resource failed: could not read file resource '%s', reason:  %+v", resourceFilePath, err)
								}
								return file, nil
							}

							// we're dealing with only here:
							resolvedResources = append(resolvedResources, resources.NewResolvedFileResourceWithPath(resourceReader,
								layerHeader.FileInfo().Mode().Perm(),
								opCopy.Source,
								filepath.Join(opCopy.Target, filepath.Base(resourceFilePath)),
								opCopy.Workdir, func() commands.User {
									if opCopy.UserFromLocalChown != nil {
										return *opCopy.UserFromLocalChown
									}
									return opCopy.User
								}(),
								resourceFilePath))

						}
					}
				} // end for prefixes

			}
		}
	}

	return resolvedResources, nil
}

func (ddb *defaultDependencyBuild) ImageRemove(tagName string) error {
	opLogger := ddb.logger.With("tag-name", tagName)
	opLogger.Debug("removing Docker stage build image")
	imageID, err := ddb.FindImageIDByTag(tagName)
	if err != nil {
		opLogger.Error("failed fetching Docker image ID by tag", tagName, "reason", err)
		return err
	}
	responses, err := ddb.dockerClient.ImageRemove(context.Background(), imageID, types.ImageRemoveOptions{Force: true})
	if err != nil {
		opLogger.Error("failed removing Docker image by",
			"image-id", imageID,
			"reason", err)
		return err
	}
	for _, response := range responses {
		opLogger.Debug("Docker image removal status",
			"image-id", imageID,
			"deleted", response.Deleted,
			"untagged", response.Untagged)
	}
	return nil
}

func (ddb *defaultDependencyBuild) FindImageIDByTag(tagName string) (string, error) {
	images, err := ddb.dockerClient.ImageList(context.Background(), types.ImageListOptions{All: true})
	if err != nil {
		return "", err
	}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == tagName {
				return img.ID, nil
			}
		}
	}
	return "", fmt.Errorf("image not found")
}

// -- docker output related types:

type dockerOutStream struct {
	Stream string `json:"stream"`
}

type dockerErrorLine struct {
	Error       string            `json:"error"`
	ErrorDetail dockerErrorDetail `json:"errorDetail"`
}

type dockerErrorDetail struct {
	Message string `json:"message"`
}

type dockerManifest struct {
	Config   string
	RepoTags interface{}
	Layers   []string
}
