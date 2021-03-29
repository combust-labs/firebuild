package resources

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/pkg/build/commands"
)

// ResolvedResource contains the data and the metadata of the resolved resource.
type ResolvedResource interface {
	Contents() (io.ReadCloser, error)
	IsDir() bool
	ResolvedURIOrPath() string

	SourcePath() string
	TargetMode() fs.FileMode
	TargetPath() string
	TargetWorkdir() commands.Workdir
	TargetUser() commands.User
}

type defaultResolvedResource struct {
	contentsReader func() (io.ReadCloser, error)
	isDir          bool
	resolved       string
	targetMode     fs.FileMode
	sourcePath     string
	targetPath     string
	targetWorkdir  commands.Workdir
	targetUser     commands.User
}

//func (drr *defaultResolvedResource) Bytes() []byte {
//	return drr.data
//}

func (drr *defaultResolvedResource) Contents() (io.ReadCloser, error) {
	return drr.contentsReader()
}

func (drr *defaultResolvedResource) IsDir() bool {
	return drr.isDir
}

func (drr *defaultResolvedResource) ResolvedURIOrPath() string {
	return drr.resolved
}

func (drr *defaultResolvedResource) SourcePath() string {
	return drr.sourcePath
}
func (drr *defaultResolvedResource) TargetMode() fs.FileMode {
	return drr.targetMode
}
func (drr *defaultResolvedResource) TargetPath() string {
	return drr.targetPath
}
func (drr *defaultResolvedResource) TargetWorkdir() commands.Workdir {
	return drr.targetWorkdir
}
func (drr *defaultResolvedResource) TargetUser() commands.User {
	return drr.targetUser
}

// -- Resource resolver:

// Resolver resolves ADD and COPY dependencies.
type Resolver interface {
	ResolveAdd(res commands.Add) ([]ResolvedResource, error)
	ResolveCopy(res commands.Copy) ([]ResolvedResource, error)
}

type defaultResolver struct {
}

// NewDefaultResolver returns a new default resolver instance.
func NewDefaultResolver() Resolver {
	return &defaultResolver{}
}

// ResolveAdd resolves an ADD command resource.
func (dr *defaultResolver) ResolveAdd(res commands.Add) ([]ResolvedResource, error) {
	return dr.resolveResources(res.OriginalSource, res.Source, res.Target, res.Workdir, func() commands.User {
		if res.UserFromLocalChown != nil {
			return *res.UserFromLocalChown
		}
		return res.User
	}())
}

// ResolveCopy resolves a COPY command resource.
func (dr *defaultResolver) ResolveCopy(res commands.Copy) ([]ResolvedResource, error) {
	return dr.resolveResources(res.OriginalSource, res.Source, res.Target, res.Workdir, func() commands.User {
		if res.UserFromLocalChown != nil {
			return *res.UserFromLocalChown
		}
		return res.User
	}())
}

func (dr *defaultResolver) resolveResources(originalSource, resourcePath, targetPath string, targetWorkdir commands.Workdir, targetUser commands.User) ([]ResolvedResource, error) {

	resources := []ResolvedResource{}

	if originalSource == "" {
		return nil, fmt.Errorf("empty: '%s' not resolvable", resourcePath)
	}

	// this here checks if the ADD relative/resource is within the same location as the https://..../Dockerfile
	if strings.HasPrefix(originalSource, "http://") || strings.HasPrefix(originalSource, "https://") {
		parent := filepath.Dir(originalSource)
		parent = strings.Replace(strings.Replace(parent, "http:/", "http://", 1), "https:/", "https://", 1)
		newPath := filepath.Join(parent, resourcePath)
		newPath = strings.Replace(strings.Replace(newPath, "http:/", "http://", 1), "https:/", "https://", 1)
		if !strings.HasPrefix(newPath, parent) {
			return nil, fmt.Errorf("http resource failed: resolved '%s' not in the context of '%s'", newPath, parent)
		}
		httpResponse, err := http.Head(newPath)
		if err != nil {
			return nil, err
		}
		defer httpResponse.Body.Close()
		if httpResponse.StatusCode%100 != 2 {
			return nil, fmt.Errorf("http resource failed: could not HEAD resource '%s', reason: %+v", newPath, err)
		}

		httpContentSupplier := func() (io.ReadCloser, error) {
			// we have the temp file:
			httpResponse, err := http.Get(newPath)
			if err != nil {
				return nil, err
			}
			return httpResponse.Body, nil
			/*
				bodyBytes, err := ioutil.ReadAll(httpResponse.Body)
				if err != nil {
					return nil, fmt.Errorf("http resource failed: could not GET resource '%s', reason: %+v", newPath, err)
				}
			*/
		}

		return append(resources, &defaultResolvedResource{contentsReader: httpContentSupplier,
			resolved:      newPath,
			targetMode:    fs.FileMode(0644),
			sourcePath:    resourcePath,
			targetPath:    targetPath,
			targetWorkdir: targetWorkdir,
			targetUser:    targetUser}), nil
	}

	// this here handles ADD / COPY (we don't distinguish) for a http source:
	if strings.HasPrefix(resourcePath, "http://") || strings.HasPrefix(resourcePath, "https://") {
		httpContentSupplier := func() (io.ReadCloser, error) {
			// we have the temp file:
			httpResponse, err := http.Get(resourcePath)
			if err != nil {
				return nil, err
			}
			return httpResponse.Body, nil
		}
		return append(resources, &defaultResolvedResource{contentsReader: httpContentSupplier,
			resolved:      resourcePath,
			targetMode:    fs.FileMode(0644),
			sourcePath:    resourcePath,
			targetPath:    targetPath,
			targetWorkdir: targetWorkdir,
			targetUser:    targetUser}), nil
	}

	newPath := filepath.Join(filepath.Dir(originalSource), resourcePath)
	if !strings.HasPrefix(newPath, filepath.Dir(originalSource)) {
		return nil, fmt.Errorf("resource failed: resolved '%s' not in the context of '%s'", newPath, filepath.Dir(originalSource))
	}

	matches, err := filepath.Glob(newPath)
	if err != nil {
		return nil, fmt.Errorf("resource failed: filepath glob error for path '%s', reason:  %+v", newPath, err)
	}

	for _, match := range matches {
		statResult, statErr := os.Stat(match)
		if statErr != nil {
			return nil, fmt.Errorf("resource failed: resolved '%s', reason: %v", match, statErr)
		}
		if statResult.IsDir() {
			resources = append(resources,
				NewResolvedDirectoryResourceWithPath(statResult.Mode().Perm(),
					newPath, resourcePath, targetPath,
					targetWorkdir,
					targetUser))
		} else {
			resources = append(resources, &defaultResolvedResource{contentsReader: func() (io.ReadCloser, error) {
				file, err := os.Open(newPath)
				if err != nil {
					return nil, fmt.Errorf("resource failed: could not read file resource '%s', reason:  %+v", newPath, err)
				}
				return file, nil
			},
				isDir:         false,
				resolved:      newPath,
				sourcePath:    resourcePath,
				targetMode:    statResult.Mode().Perm(),
				targetPath:    targetPath,
				targetWorkdir: targetWorkdir,
				targetUser:    targetUser})
		}
	}

	return resources, nil
}

// NewResolvedFileResource creates a resolved resource from input information.
func NewResolvedFileResource(contentsReader func() (io.ReadCloser, error), mode fs.FileMode, sourcePath, targetPath string, workdir commands.Workdir, user commands.User) ResolvedResource {
	return NewResolvedFileResourceWithPath(contentsReader, mode, sourcePath, targetPath, workdir, user, "")
}

// NewResolvedFileResourceWithPath creates a resolved resource from input information containing resource source path.
func NewResolvedFileResourceWithPath(contentsReader func() (io.ReadCloser, error), mode fs.FileMode, sourcePath, targetPath string, workdir commands.Workdir, user commands.User, path string) ResolvedResource {
	return &defaultResolvedResource{contentsReader: contentsReader,
		isDir:         false,
		resolved:      path,
		targetMode:    mode,
		sourcePath:    sourcePath,
		targetPath:    targetPath,
		targetWorkdir: workdir,
		targetUser:    user}
}

// NewResolvedDirectoryResourceWithPath creates a resolved resource from input information containing resource source path.
func NewResolvedDirectoryResourceWithPath(mode fs.FileMode, resolvedPath, sourcePath, targetPath string, workdir commands.Workdir, user commands.User) ResolvedResource {
	return &defaultResolvedResource{contentsReader: func() (io.ReadCloser, error) {
		// TODO-MULTI-STAGE-VMINIT: here an SCP like protocol is needed to read the contents of the directory in one go
		fmt.Println(" =====================> directory ", resolvedPath)
		return ioutil.NopCloser(bytes.NewReader([]byte{})), nil
	},
		isDir:         true,
		resolved:      resolvedPath,
		targetMode:    mode,
		sourcePath:    sourcePath,
		targetPath:    targetPath,
		targetWorkdir: workdir,
		targetUser:    user}
}
