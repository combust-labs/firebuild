package resources

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/appministry/firebuild/build/commands"
)

// ResolvedResource contains the data and the metadata of the resolved resource.
type ResolvedResource interface {
	Bytes() []byte
	IsDir() bool
	ResolvedURIOrPath() string

	TargetMode() fs.FileMode
	TargetPath() string
	TargetWorkdir() commands.Workdir
	TargetUser() commands.User
}

type defaultResolvedResource struct {
	data     []byte
	isDir    bool
	resolved string

	targetMode    fs.FileMode
	targetPath    string
	targetWorkdir commands.Workdir
	targetUser    commands.User
}

func (drr *defaultResolvedResource) Bytes() []byte {
	return drr.data
}

func (drr *defaultResolvedResource) IsDir() bool {
	return drr.isDir
}

func (drr *defaultResolvedResource) ResolvedURIOrPath() string {
	return drr.resolved
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
	if strings.HasPrefix(originalSource, "http://") || strings.HasPrefix(originalSource, "https://") {
		parent := filepath.Dir(originalSource)
		parent = strings.Replace(strings.Replace(parent, "http:/", "http://", 1), "https:/", "https://", 1)
		newPath := filepath.Join(parent, resourcePath)
		newPath = strings.Replace(strings.Replace(newPath, "http:/", "http://", 1), "https:/", "https://", 1)
		if !strings.HasPrefix(newPath, parent) {
			return nil, fmt.Errorf("http resource failed: resolved '%s' not in the context of '%s'", newPath, parent)
		}
		// we have the temp file:
		httpResponse, err := http.Get(newPath)
		if err != nil {
			return nil, err
		}
		defer httpResponse.Body.Close()
		bodyBytes, err := ioutil.ReadAll(httpResponse.Body)
		if err != nil {
			return nil, fmt.Errorf("http resource failed: could not GET resource '%s', reason: %+v", newPath, err)
		}
		return append(resources, &defaultResolvedResource{data: bodyBytes,
			resolved:      newPath,
			targetMode:    fs.FileMode(0644),
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
			resources = append(resources, &defaultResolvedResource{data: []byte{},
				isDir:         true,
				resolved:      newPath,
				targetMode:    statResult.Mode(),
				targetPath:    targetPath,
				targetWorkdir: targetWorkdir,
				targetUser:    targetUser})
		} else {
			fileBytes, err := ioutil.ReadFile(newPath)
			if err != nil {
				return nil, fmt.Errorf("resource failed: could not read resource '%s', reason:  %+v", newPath, err)
			}
			resources = append(resources, &defaultResolvedResource{data: fileBytes,
				isDir:         false,
				resolved:      newPath,
				targetMode:    statResult.Mode(),
				targetPath:    targetPath,
				targetWorkdir: targetWorkdir,
				targetUser:    targetUser})
		}
	}

	return resources, nil
}

// NewResolvedFileResource creates a resolved resource from input information.
func NewResolvedFileResource(data []byte, mode fs.FileMode, targetPath string, workdir commands.Workdir, user commands.User) ResolvedResource {
	return &defaultResolvedResource{data: data,
		isDir:         false,
		resolved:      "",
		targetMode:    mode,
		targetPath:    targetPath,
		targetWorkdir: workdir,
		targetUser:    user}
}
