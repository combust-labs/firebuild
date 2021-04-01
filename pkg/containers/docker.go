package containers

import (
	tar "archive/tar"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/combust-labs/firebuild-shared/build/commands"
	"github.com/combust-labs/firebuild-shared/build/resources"
	"github.com/combust-labs/firebuild/pkg/build/stage"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/hashicorp/go-hclog"
	"github.com/opentracing/opentracing-go"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/strslice"
	docker "github.com/docker/docker/client"
	dockerArchive "github.com/docker/docker/pkg/archive"
)

var (
	// ContainerStopTimeout is the amount of time the container is given to stop gracefully.
	ContainerStopTimeout = time.Duration(time.Second * 30)
	// ImageBaseOSExportCommand is the command to execute when starting the base OS file system export container.
	ImageBaseOSExportCommand = []string{"/bin/sh"}
	// ImageBaseOSExportExecShell is the shell used to execute the docker exec commands.
	ImageBaseOSExportExecShell = []string{"/bin/sh", "-c"}
	// ImageBaseOSExportFsCopyExecTimeout is the amount of time the exec command has to work on the base operating system file system copy.
	ImageBaseOSExportFsCopyExecTimeout = time.Duration(time.Second * 15)
	// ImageBaseOSExportMountTarget is the path under which the volume where the file system is exported to will be mounted in the container.
	ImageBaseOSExportMountTarget = "/export-rootfs"
	// ImageBaseOSExportNoCopyDirs is a list of base operating system exported file system directories
	// for which no contents must be copied, the directory must only be created.
	// These are used in the following way:
	// - for every directory in base OS file system root (`find / -maxdepth 1 -type d`)
	//   - if directory exists in the list, just create the directory
	//   - if directory does not exist in the list and is not /, copy complete by preserving inode attributes
	ImageBaseOSExportNoCopyDirs = []string{"/boot", "/opt", "/proc", "/run", "/srv", "/sys", "/tmp"}
)

// GetDefaultClient returns a default instance of the Docker client.
func GetDefaultClient() (*docker.Client, error) {
	return docker.NewEnvClient()
}

// FindImageIDByTag looks up the Docker image ID given a tag name.
func FindImageIDByTag(ctx context.Context, client *docker.Client, requiredTag string) (string, error) {
	images, err := client.ImageList(ctx, types.ImageListOptions{All: true})
	if err != nil {
		return "", err
	}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == requiredTag {
				return img.ID, nil
			}
		}
	}
	return "", fmt.Errorf("image not found")
}

// ImageBaseOSExport exports the base operating system file system.
// It does so by starting the container with a bind volume pointing to the host directory defined by `path`.
// The `path` should point at a mounted ext4 file system such that, when the file system is copied, the ext4 file
// contains the contents of the base OS Docker image.
// The contents are copied via docker exec commands.
// Once the file system is exported, the function stops the container and removes it.
func ImageBaseOSExport(ctx context.Context, client *docker.Client, logger hclog.Logger, path, tagName string,
	tracer opentracing.Tracer, spanContext opentracing.SpanContext) error {

	opLogger := logger.With("tag-name", tagName)

	cleanup := utils.NewDefers()
	defer cleanup.CallAll()

	containerConfig := &container.Config{
		OpenStdin: true,
		Tty:       true,
		Cmd:       strslice.StrSlice(ImageBaseOSExportCommand),
		Image:     tagName,
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: path,
				Target: ImageBaseOSExportMountTarget,
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

	containerCreateResponse, startErr := client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if startErr != nil {
		opLogger.Error("failed creating a Docker container", "reason", startErr)
		return startErr
	}

	cleanup.Add(func() {
		span := tracer.StartSpan("docker-remove-container", opentracing.ChildOf(spanContext))
		span.SetTag("container-id", containerCreateResponse.ID)
		removeContainer(context.Background(), client, logger, containerCreateResponse.ID)
		span.Finish()
	})

	opLogger = opLogger.With("container-id", containerCreateResponse.ID)
	opLogger.Debug("container started")

	if err := client.ContainerStart(ctx, containerCreateResponse.ID, types.ContainerStartOptions{}); err != nil {
		opLogger.Error("failed starting a Docker container", "reason", err)
		return err
	}

	cleanup.Add(func() {
		span := tracer.StartSpan("docker-stop-container", opentracing.ChildOf(spanContext))
		span.SetTag("container-id", containerCreateResponse.ID)
		stopContainer(context.Background(), client, logger, containerCreateResponse.ID)
		span.Finish()
	})

	bareList := strings.Join(ImageBaseOSExportNoCopyDirs, " ")
	dirsNoCopyList := "LIST=\"" + strings.Join([]string{"/", ImageBaseOSExportMountTarget}, " ") + bareList + "\"; "
	mkdirOnlyDirsStr := "LIST=\"" + bareList + "\"; "

	commands := []string{
		// these ones should have an empty directory only:
		mkdirOnlyDirsStr + "for d in $(find / -maxdepth 1 -type d); do if echo $LIST | grep -w $d > /dev/null; then mkdir " + ImageBaseOSExportMountTarget + "${d}; fi; done; exit 0",
		// these are the ones I want to copy, when they don't exist in the list:
		dirsNoCopyList + "for d in $(find / -maxdepth 1 -type d); do if echo $LIST | grep -v -w $d > /dev/null; then tar c \"$d\" | tar x -C " + ImageBaseOSExportMountTarget + "; fi; done",
		// clean up:
		fmt.Sprintf("rm -r %s/%s", ImageBaseOSExportMountTarget, ImageBaseOSExportMountTarget),
	}

	for idx, command := range commands {

		opLogger.Debug(fmt.Sprintf("running exec %d of %d", idx+1, len(commands)))

		execIDResponse, execErr := client.ContainerExecCreate(ctx, containerCreateResponse.ID, types.ExecConfig{
			AttachStdout: true,
			AttachStderr: true,
			Cmd: func() []string {
				cmd := ImageBaseOSExportExecShell
				return append(cmd, command)
			}(),
		})
		if execErr != nil {
			opLogger.Error("error creating exec", "container-id", containerCreateResponse.ID, "reason", execErr)
			return execErr
		}

		hijackedConn, execAttachErr := client.ContainerExecAttach(ctx, execIDResponse.ID, types.ExecStartCheck{
			Tty: true,
		})
		if execAttachErr != nil {
			opLogger.Error("error attaching exec", "reason", execAttachErr)
			return execAttachErr
		}

		chanDone := make(chan struct{}, 1)
		chanError := make(chan error, 1)
		execReadCtx, execReadCtxCancelFunc := context.WithTimeout(ctx, ImageBaseOSExportFsCopyExecTimeout)
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

	return nil
}

// ImageBuild builds a Docker image in the context os source directory, using Dockerfile from dockerfilePath
// and tags the image as tag.
func ImageBuild(ctx context.Context, client *docker.Client, logger hclog.Logger, source, dockerfilePath, tagName string) error {

	if !strings.HasSuffix(source, "/") {
		source = fmt.Sprintf("%s/", source)
	}

	opLogger := logger.With("dir-context", source, "dockerfile", dockerfilePath, "tag-name", tagName)

	// convert the context into a tar:
	tar, err := dockerArchive.TarWithOptions(source, &dockerArchive.TarOptions{})
	if err != nil {
		opLogger.Error("failed creating tar archive as Docker build context", "reason", err)
		return err
	}
	defer tar.Close()

	// build the image:
	buildResponse, buildErr := client.ImageBuild(ctx, tar, types.ImageBuildOptions{
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

// ImageExportStageDependentResources exports resources from a given Docker image indicated by tag.
// The resources are exported only when there is a command.Copy with the `--from` flag poiting at this container.
// This function opens the Docker image tar file system and reads every layer individually in search of a resource.
// If the resource is founnd in a layer, it is added to the list of returned resolved resources.
// If the resource points at a directory, the function lists the contents of the directory and returns an item for every
// resource, the function does not return resolved resources pointing at directories.
func ImageExportStageDependentResources(ctx context.Context, client *docker.Client, logger hclog.Logger,
	stage stage.Stage,
	exportsRoot string, externalCopies []commands.Copy, tagName string) ([]resources.ResolvedResource, error) {

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

	opLogger := logger.With("exports-root", exportsRoot, "tag-name", tagName)

	resolvedResources := []resources.ResolvedResource{}
	opCopies := []commands.Copy{}

	for _, externalCopy := range externalCopies {
		if externalCopy.Stage != stage.Name() {
			continue
		}
		opCopies = append(opCopies, externalCopy)
	}

	if len(opCopies) == 0 {
		return resolvedResources, nil // shortcircuit, nothing to look up
	}
	opLogger.Debug("exporting Docker stage build image")
	imageID, err := FindImageIDByTag(ctx, client, tagName)
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

	reader, err := client.ImageSave(ctx, []string{imageID})
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
							// --
							// CONSIDER: saving the files in a stage derived directory:
							// --
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

// ImageRemove removes the Docker image using the tag name.
func ImageRemove(ctx context.Context, client *docker.Client, logger hclog.Logger, tagName string) error {
	opLogger := logger.With("tag-name", tagName)
	opLogger.Debug("removing Docker stage build image")
	imageID, err := FindImageIDByTag(ctx, client, tagName)
	if err != nil {
		opLogger.Error("failed fetching Docker image ID by tag", tagName, "reason", err)
		return err
	}
	responses, err := client.ImageRemove(ctx, imageID, types.ImageRemoveOptions{Force: true})
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

func removeContainer(ctx context.Context, client *docker.Client, opLogger hclog.Logger, containerID string) {
	opLogger.Debug("removing container")
	containerRemoveOptions := types.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	}
	go func() {
		if removeError := client.ContainerRemove(ctx, containerID, containerRemoveOptions); removeError != nil {
			opLogger.Warn("problem removing the container", "reason", removeError)
		}
	}()
	opLogger.Debug("waiting for container to be removed")
	chanRemoveOK, chanRemoveErr := client.ContainerWait(ctx, containerID, container.WaitConditionRemoved)
	select {
	case ok := <-chanRemoveOK:
		opLogger.Debug("container removed", "exit-code", ok.StatusCode, "error-message", ok.Error)
	case removeError := <-chanRemoveErr:
		opLogger.Warn("container stop wait returned an error", "reason", removeError)
	}
}

func stopContainer(ctx context.Context, client *docker.Client, opLogger hclog.Logger, containerID string) {
	opLogger.Debug("stopping container")
	go func() {
		if stopError := client.ContainerStop(ctx, containerID, &ContainerStopTimeout); stopError != nil {
			opLogger.Warn("problem stopping the container gracefully, killing", "reason", stopError)
			if killError := client.ContainerKill(ctx, containerID, "SIGKILL"); killError != nil {
				opLogger.Warn("container kill also returned an error", "reason", killError)
			}
		}
	}()
	opLogger.Debug("waiting for container to stop")
	chanStopOK, chanStopErr := client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case ok := <-chanStopOK:
		opLogger.Debug("container stopped", "exit-code", ok.StatusCode, "error-message", ok.Error)
	case stopErr := <-chanStopErr:
		opLogger.Warn("container stop wait returned an error", "reason", stopErr)
	}
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
