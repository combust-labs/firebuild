package build

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/docker/docker/pkg/fileutils"
)

func mustNewArg(t *testing.T, rawValue string) commands.Arg {
	arg, err := commands.NewRawArg(rawValue)
	if err != nil {
		t.Fatal(err)
	}
	return arg
}

func TestContextBuilderSingleStageWithResources(t *testing.T) {

	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("expected temp dir, got error", err)
	}
	defer os.RemoveAll(tempDir)
	if err := ioutil.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte(testDockerfile1), fs.ModePerm); err != nil {
		t.Fatal("expected Dockerfile to be written, got error", err)
	}
	if err := ioutil.WriteFile(filepath.Join(tempDir, "resource1"), []byte("resource 1 content"), fs.ModePerm); err != nil {
		t.Fatal("expected Dockerfile to be written, got error", err)
	}
	if err := ioutil.WriteFile(filepath.Join(tempDir, "resource2"), []byte("resource 2 content"), fs.ModePerm); err != nil {
		t.Fatal("expected Dockerfile to be written, got error", err)
	}
	readResult, err := reader.ReadFromString(filepath.Join(tempDir, "Dockerfile"), tempDir)
	if err != nil {
		t.Fatal("expected Dockerfile to be read, got error", err)
	}

	contextBuilder := NewDefaultBuild()
	if err := contextBuilder.AddInstructions(readResult.Commands()...); err != nil {
		t.Fatal("expected commands to be added, got error", err)
	}
	buildCtx, err := contextBuilder.WithResolver(resources.NewDefaultResolver()).CreateContext(make(Resources))
	if err != nil {
		t.Fatal("expected build context to be created, got error", err)
	}

	t.Log(buildCtx)
}

func TestDockerignoreMatches(t *testing.T) {
	patternMatcher, err := fileutils.NewPatternMatcher([]string{
		".DS_Store",
		"**/.DS_Store",
		"vendor/",
		"**/vendor",
		"!internal/vendor",
		"data/**",
		"!data/must-exist",
	})
	if err != nil {
		t.Fatal("Expected pattern matcher to be created")
	}

	tests := map[string]bool{
		".DS_Store":            true,
		"aaa/bbb/.DS_Store":    true,
		"aaa/bbb/.DS_Storeeee": false,
		"vendor/aaa/bbb/ccc":   true,
		"vendor":               true,
		"someother/vendor":     true,
		"internal/vendor":      false,
		"data/aaa/bbb":         true,
		"data/must-exist":      false,
	}

	for k, v := range tests {
		status, _ := patternMatcher.Matches(k)
		if status != v {
			t.Error("Expected != result for path", k, fmt.Sprintf("%v vs %v", v, status))
		}
	}
}

const testDockerfile1 = `FROM alpine:3.13
ARG PARAM1=value
ENV ENVPARAM1=ebvparam1
ADD resource1 /target/resource1
COPY resource2 /target/resource2
RUN cp /dir/${ENVPARAM1} \
	&& call --arg=${PARAM1}`
