package resources

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/appministry/firebuild/buildcontext/commands"
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
	ResolveAdd(res commands.Add) (ResolvedResource, error)
	ResolveCopy(res commands.Copy) (ResolvedResource, error)
}

type defaultResolver struct {
}

// NewDefaultResolver returns a new default resolver instance.
func NewDefaultResolver() Resolver {
	return &defaultResolver{}
}

// ResolveAdd resolves an ADD command resource.
func (dr *defaultResolver) ResolveAdd(res commands.Add) (ResolvedResource, error) {
	return dr.resolveResource(res.OriginalSource, res.Source, res.Target, res.Workdir, res.User)
}

// ResolveCopy resolves a COPY command resource.
func (dr *defaultResolver) ResolveCopy(res commands.Copy) (ResolvedResource, error) {
	return dr.resolveResource(res.OriginalSource, res.Source, res.Target, res.Workdir, res.User)
}

func (dr *defaultResolver) resolveResource(originalSource, resourcePath, targetPath string, targetWorkdir commands.Workdir, targetUser commands.User) (ResolvedResource, error) {
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
		return &defaultResolvedResource{data: bodyBytes,
			resolved:      newPath,
			targetMode:    fs.FileMode(0644),
			targetPath:    targetPath,
			targetWorkdir: targetWorkdir,
			targetUser:    targetUser}, nil
	}

	newPath := filepath.Join(filepath.Dir(originalSource), resourcePath)
	if !strings.HasPrefix(newPath, originalSource) {
		return nil, fmt.Errorf("resource failed: resolved '%s' not in the context of '%s'", newPath, originalSource)
	}
	statResult, statErr := os.Stat(newPath)
	if statErr != nil {
		if statErr == os.ErrNotExist {
			return nil, fmt.Errorf("resource failed: resolved '%s' not found locally", newPath)
		}
	}
	if statResult.IsDir() {
		return &defaultResolvedResource{data: []byte{},
			isDir:         true,
			resolved:      newPath,
			targetMode:    statResult.Mode(),
			targetPath:    targetPath,
			targetWorkdir: targetWorkdir,
			targetUser:    targetUser}, nil
	}
	fileBytes, err := ioutil.ReadFile(newPath)
	if err != nil {
		return nil, fmt.Errorf("resource failed: could not read resource '%s', reason:  %+v", newPath, err)
	}

	return &defaultResolvedResource{data: fileBytes,
		isDir:         false,
		resolved:      newPath,
		targetMode:    statResult.Mode(),
		targetPath:    targetPath,
		targetWorkdir: targetWorkdir,
		targetUser:    targetUser}, nil
}
