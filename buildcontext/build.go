package buildcontext

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/appministry/firebuild/buildcontext/commands"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	git "github.com/go-git/go-git/v5"
)

// NewFromString creates a new build context from string.
//
// - literal Dockerfile content, ADD and COPY will not work
// - http:// or httpL// URL
// - git+http:// and git+https:// URL
//   the format is: git+http(s)://host:port/path/to/repo.git:/path/to/Dockerfile[#<commit-hash | branch-name | tag-name>]
// - absolute path to the local file
func NewFromString(input string, tempDirectory string) (Build, error) {

	if strings.HasPrefix(input, "git+http://") || strings.HasPrefix(input, "git+https://") {
		u, _ := url.Parse(input)

		branchToCheckout := u.Fragment
		pathParts := strings.Split(u.Path, ":")
		if len(pathParts) != 2 {
			return nil, fmt.Errorf("invalid path: %s, expected /org/repo.git:/file/in/repo", u.Path)
		}

		pathInRepo := pathParts[1]
		u.Path = pathParts[0]
		u.Fragment = ""

		repoURL := strings.Replace(strings.Replace(u.String(), "git+http://", "http://", 1), "git+https://", "https://", 1)

		repoDestDir := filepath.Join(tempDirectory, "sources")
		repo, err := git.PlainClone(repoDestDir, false, &git.CloneOptions{
			URL:      repoURL,
			Progress: os.Stdout,
		})
		if err != nil {
			return nil, fmt.Errorf("failed clone: %+v", err)
		}

		if branchToCheckout != "" {
			remotes, err := repo.Remotes()
			if err != nil {
				return nil, fmt.Errorf("failed listing remotes: %+v", err)
			}
			refs, err := remotes[0].List(&git.ListOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed listing remotes: %+v", err)
			}
			for _, ref := range refs {
				if ref.Name().IsBranch() || ref.Name().IsTag() {
					if ref.Hash().String() == branchToCheckout || strings.HasSuffix(ref.Name().String(), fmt.Sprintf("/%s", branchToCheckout)) {
						worktree, err := repo.Worktree()
						if err != nil {
							return nil, fmt.Errorf("failed fetching worktree: %+v", err)
						}
						if err := worktree.Checkout(&git.CheckoutOptions{
							Hash: ref.Hash(),
						}); err != nil {
							return nil, fmt.Errorf("failed checkout %s: %+v", branchToCheckout, err)
						}
						break
					}
				}
			}
		}

		// the Dockerfile is basically:
		filePath := filepath.Join(repoDestDir, pathInRepo)
		statResult, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, statErr
		}
		if statResult.IsDir() {
			return nil, &ErrorIsDirectory{Input: filePath}
		}
		bytes, err := ioutil.ReadFile(filePath)
		if err != nil && err != io.EOF {
			return nil, err
		}

		return NewFromBytesWithOriginalSource(bytes, filePath)
	}

	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		httpResponse, err := http.Get(input)
		if err != nil {
			return nil, err
		}
		defer httpResponse.Body.Close()
		bytes, err := ioutil.ReadAll(httpResponse.Body)
		if err != nil && err != io.EOF {
			return nil, err
		}
		return NewFromBytesWithOriginalSource(bytes, input)
	}

	statResult, statErr := os.Stat(input)
	if statErr != nil {

		if statErr == os.ErrNotExist {
			// assume literal input:
			return NewFromBytes([]byte(input))
		}

		if statErr != os.ErrNotExist {
			return nil, statErr
		}
	}

	if statResult.IsDir() {
		return nil, &ErrorIsDirectory{Input: input}
	}

	bytes, err := ioutil.ReadFile(input)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return NewFromBytesWithOriginalSource(bytes, input)

}

// NewFromBytes creates a new build context from bytes.
// The bytes most often will be the Dockerfile string literal converted to bytes.
func NewFromBytes(input []byte) (Build, error) {
	parserResult, err := parser.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	return NewFromParserResult(parserResult, "")
}

// NewFromBytesWithOriginalSource creates a new build context from bytes and passes
// the original source to the build context.
// Use this method to automatically resolve the ADD / COPY dependencies.
func NewFromBytesWithOriginalSource(input []byte, originalSource string) (Build, error) {
	parserResult, err := parser.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	return NewFromParserResult(parserResult, originalSource)
}

// NewFromParserResult creates a new build context from the Dockerfile parser result.
func NewFromParserResult(parserResult *parser.Result, originalSource string) (Build, error) {
	buildContext := NewDefaultBuild()
	env := newBuildEnv()
	for _, child := range parserResult.AST.Children {
		switch child.Value {
		case "add":
			extracted := []string{}
			current := child.Next
			for {
				if current == nil {
					break
				}
				extracted = append(extracted, current.Value)
				current = current.Next
			}
			if len(extracted) != 2 {
				return nil, fmt.Errorf("the ADD at %d must have exactly 2 elements", child.StartLine)
			}
			buildContext.WithInstruction(commands.Add{
				OriginalSource: originalSource,
				Source:         extracted[0],
				Target:         extracted[1],
			})
		case "arg":
			extracted := []string{}
			current := child.Next
			for {
				if current == nil {
					break
				}
				extracted = append(extracted, current.Value)
				current = current.Next
			}
			for i := 0; i < len(extracted); i++ {
				argsParts := strings.Split(extracted[i], "=")
				if len(argsParts) == 0 {
					return nil, fmt.Errorf("the arg at %d is empty", child.StartLine)
				}
				name, value := env.put(argsParts[0], strings.Join(argsParts[1:], "="))
				buildContext.WithInstruction(commands.Arg{
					Name:  name,
					Value: value,
				})
			}
		case "cmd":
			cmd := commands.Cmd{Values: []string{}}
			current := child.Next
			for {
				if current == nil {
					break
				}
				cmd.Values = append(cmd.Values, current.Value)
				current = current.Next
			}
			buildContext.WithInstruction(cmd)
		case "copy":
			extracted := []string{}
			current := child.Next
			for {
				if current == nil {
					break
				}
				extracted = append(extracted, current.Value)
				current = current.Next
			}
			if len(extracted) != 2 {
				return nil, fmt.Errorf("the cmd at %d must have exactly 2 elements", child.StartLine)
			}
			buildContext.WithInstruction(commands.Copy{
				OriginalSource: originalSource,
				Source:         extracted[0],
				Target:         extracted[1],
			})
		case "entrypoint":
			entrypoint := commands.Entrypoint{Values: []string{}}
			current := child.Next
			for {
				if current == nil {
					break
				}
				entrypoint.Values = append(entrypoint.Values, current.Value)
				current = current.Next
			}
			buildContext.WithInstruction(entrypoint)
		case "env":
			extracted := []string{}
			current := child.Next
			for {
				if current == nil {
					break
				}
				extracted = append(extracted, current.Value)
				current = current.Next
			}
			if len(extracted)%2 != 0 {
				return nil, fmt.Errorf("the env at %d is not complete", child.StartLine)
			}
			for i := 0; i < len(extracted); i = i + 2 {
				name, value := env.put(extracted[i], extracted[i+1])
				buildContext.WithInstruction(commands.Env{
					Name:  name,
					Value: value,
				})
			}
		case "expose":
			current := child.Next
			for {
				if current == nil {
					break
				}
				buildContext.WithInstruction(commands.Expose{RawValue: current.Value})
				current = current.Next
			}
		case "from":
			if child.Next == nil {
				return nil, fmt.Errorf("expected from value")
			}
			buildContext.WithFrom(&commands.From{BaseImage: child.Next.Value})
		case "healthcheck":
			// ignore for now
			// TODO: these can be for sure used but at a higher level
		case "label":
			extracted := []string{}
			current := child.Next
			for {
				if current == nil {
					break
				}
				extracted = append(extracted, current.Value)
				current = current.Next
			}
			if len(extracted)%2 != 0 {
				return nil, fmt.Errorf("the label at %d is not complete", child.StartLine)
			}
			for i := 0; i < len(extracted); i = i + 2 {
				buildContext.WithInstruction(commands.Label{
					Key:   extracted[i],
					Value: env.expand(extracted[i+1]),
				})
			}
		case "maintainer":
			// ignore, it's deprectaed
		case "onbuild":
			// ignore for now
			// TODO: can these be used?
		case "run":
			current := child.Next
			for {
				if current == nil {
					break
				}
				buildContext.WithInstruction(commands.Run{
					Command: env.expand(current.Value),
				})
				current = current.Next
			}
		case "shell":
			shell := commands.Shell{Commands: []string{}}
			current := child.Next
			for {
				if current == nil {
					break
				}
				shell.Commands = append(shell.Commands, current.Value)
				current = current.Next
			}
			buildContext.WithInstruction(shell)
		case "stopsignal":
			// TODO: incorporate because the OS service manager can take advantage of this
		case "user":
			if child.Next == nil {
				return nil, fmt.Errorf("expected user value")
			}
			buildContext.WithInstruction(commands.User{Value: child.Next.Value})
		case "volume":
			vols := commands.Volume{Values: []string{}}
			current := child.Next
			for {
				if current == nil {
					break
				}
				vols.Values = append(vols.Values, current.Value)
				current = current.Next
			}
			buildContext.WithInstruction(vols)
		case "workdir":
			if child.Next == nil {
				return nil, fmt.Errorf("expected workdir value")
			}
			buildContext.WithInstruction(commands.Workdir{Value: child.Next.Value})
		}
	}

	return buildContext, nil
}
