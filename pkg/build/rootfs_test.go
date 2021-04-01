package build

import (
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/combust-labs/firebuild-shared/build/commands"
	"github.com/combust-labs/firebuild-shared/build/resources"
	"github.com/combust-labs/firebuild-shared/build/rootfs"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/stage"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/hashicorp/go-hclog"

	"github.com/stretchr/testify/assert"
)

func TestContextBuilderMultiStageWithResources(t *testing.T) {
	logger := hclog.Default()
	logger.SetLevel(hclog.Debug)

	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("expected temp dir, got error", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")

	expectedResource1Bytes := []byte("resource 1 content")
	expectedResource2Bytes := []byte("resource 2 content")

	mustPutTestResource(t, dockerfilePath, []byte(testDockerfileMultiStage))
	mustPutTestResource(t, filepath.Join(tempDir, "resource1"), []byte(expectedResource1Bytes))
	mustPutTestResource(t, filepath.Join(tempDir, "resource2"), []byte(expectedResource2Bytes))

	readResult, err := reader.ReadFromString(dockerfilePath, tempDir)
	if err != nil {
		t.Fatal("expected Dockerfile to be read, got error", err)
	}

	// this is a multi stage build so let's test the stage selection:
	stages, errs := stage.ReadStages(readResult.Commands())
	if len(errs) > 0 {
		t.Fatal("expected no errors in stage reader, got", errs)
	}

	unnamed := stages.Unnamed()
	if len(unnamed) != 1 {
		t.Fatal("expected exactly one unnamed stage, got", len(unnamed))
	}

	named := stages.Named()
	if len(unnamed) != 1 {
		t.Fatal("expected exactly one named stage, got", len(named))
	}

	contextBuilder := NewDefaultBuild()
	if err := contextBuilder.AddInstructions(unnamed[0].Commands()...); err != nil {
		t.Fatal("expected commands to be added, got error", err)
	}

	t.Run("it=fails if dependency resources do not exist", func(tt *testing.T) {
		_, err := contextBuilder.WithResolver(resources.NewDefaultResolver()).CreateContext(make(rootfs.Resources))
		if err == nil {
			tt.Fatal("expected context creation to fail, but it built", err)
		}
	})

	t.Run("it=succeeds when dependency resources exist", func(tt *testing.T) {

		mustPutTestResource(tt, filepath.Join(tempDir, "etc/test/file1"), []byte("etc/test/file1"))
		mustPutTestResource(tt, filepath.Join(tempDir, "etc/test/file2"), []byte("etc/test/file2"))
		mustPutTestResource(tt, filepath.Join(tempDir, "etc/test/subdir/subdir-file1"), []byte("etc/test/subdir/subdir-file1"))

		// construct resolved resources from the written files:
		dependencyResources := rootfs.Resources{
			"builder": []resources.ResolvedResource{resources.
				NewResolvedDirectoryResourceWithPath(fs.ModePerm,
					filepath.Join(tempDir, "etc/test"), "/etc/test", "/etc/test",
					commands.Workdir{Value: "/"}, commands.User{Value: "0:0"}),
			},
		}

		buildCtx, err := contextBuilder.WithResolver(resources.NewDefaultResolver()).CreateContext(dependencyResources)
		if err != nil {
			tt.Fatal("expected build context to be created, got error", err)
		}

		testServer, testClient, cancelFunc := rootfs.MustStartTestGRPCServer(tt, logger, buildCtx)
		defer cancelFunc()

		opErr := testClient.Commands()
		if opErr != nil {
			tt.Fatal("GRPC client Commands() opErr", opErr)
		}

		mustBeRunCommand(tt, testClient)
		mustBeAddCommand(tt, testClient, expectedResource1Bytes)
		mustBeCopyCommand(tt, testClient, expectedResource2Bytes)
		// directories do not have a byte content, they always return empty bytes:
		// since we have:
		// - /etc/test: dir
		// - /etc/test/file1: file
		// - /etc/test/file2: file
		// - /etc/test/subdir: dir
		// - /etc/test/subdir/subdir-file1: file
		// we expect the following:
		mustBeCopyCommand(tt, testClient, []byte{}, []byte("etc/test/file1"), []byte("etc/test/file2"), []byte{}, []byte("etc/test/subdir/subdir-file1"))
		mustBeRunCommand(tt, testClient)
		assert.Nil(tt, testClient.NextCommand())

		testClient.Success()
		<-testServer.FinishedNotify()

	})

}

func TestContextBuilderSingleStageWithResources(t *testing.T) {

	logger := hclog.Default()
	logger.SetLevel(hclog.Debug)

	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("expected temp dir, got error", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")

	expectedResource1Bytes := []byte("resource 1 content")
	expectedResource2Bytes := []byte("resource 2 content")

	mustPutTestResource(t, dockerfilePath, []byte(testDockerfileSingleStage))
	mustPutTestResource(t, filepath.Join(tempDir, "resource1"), []byte(expectedResource1Bytes))
	mustPutTestResource(t, filepath.Join(tempDir, "resource2"), []byte(expectedResource2Bytes))

	readResult, err := reader.ReadFromString(dockerfilePath, tempDir)
	if err != nil {
		t.Fatal("expected Dockerfile to be read, got error", err)
	}

	contextBuilder := NewDefaultBuild()
	if err := contextBuilder.AddInstructions(readResult.Commands()...); err != nil {
		t.Fatal("expected commands to be added, got error", err)
	}

	buildCtx, err := contextBuilder.WithResolver(resources.NewDefaultResolver()).CreateContext(make(rootfs.Resources))
	if err != nil {
		t.Fatal("expected build context to be created, got error", err)
	}

	testServer, testClient, cancelFunc := rootfs.MustStartTestGRPCServer(t, logger, buildCtx)
	defer cancelFunc()

	opErr := testClient.Commands()
	if opErr != nil {
		t.Fatal("GRPC client Commands() opErr", opErr)
	}

	mustBeRunCommand(t, testClient)
	mustBeAddCommand(t, testClient, expectedResource1Bytes)
	mustBeCopyCommand(t, testClient, expectedResource2Bytes)
	mustBeRunCommand(t, testClient)
	assert.Nil(t, testClient.NextCommand())

	testClient.Success()
	<-testServer.FinishedNotify()
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

func mustPutTestResource(t *testing.T, path string, contents []byte) {
	if err := os.MkdirAll(filepath.Dir(path), fs.ModePerm); err != nil {
		t.Fatal("failed creating parent directory for the resource, got error", err)
	}
	if err := ioutil.WriteFile(path, contents, fs.ModePerm); err != nil {
		t.Fatal("expected resource to be written, got error", err)
	}
}

func mustReadFromReader(reader io.ReadCloser, _ error) ([]byte, error) {
	return ioutil.ReadAll(reader)
}

func mustBeAddCommand(t *testing.T, testClient rootfs.ClientProvider, expectedContents ...[]byte) {
	if addCommand, ok := testClient.NextCommand().(commands.Add); !ok {
		t.Fatal("expected ADD command")
	} else {
		mustReadResources(t, testClient, addCommand.Source, expectedContents...)

	}
}

func mustBeCopyCommand(t *testing.T, testClient rootfs.ClientProvider, expectedContents ...[]byte) {
	if copyCommand, ok := testClient.NextCommand().(commands.Copy); !ok {
		t.Fatal("expected COPY command")
	} else {
		mustReadResources(t, testClient, copyCommand.Source, expectedContents...)
	}
}

func mustReadResources(t *testing.T, testClient rootfs.ClientProvider, source string, expectedContents ...[]byte) {
	resourceChannel, err := testClient.Resource(source)
	if err != nil {
		t.Fatal("expected resource channel for COPY command, got error", err)
	}

	idx := 0
out:
	for {
		select {
		case item := <-resourceChannel:
			switch titem := item.(type) {
			case nil:
				break out // break out on nil
			case resources.ResolvedResource:
				resourceData, err := mustReadFromReader(titem.Contents())
				if err != nil {
					t.Fatal("expected resource to read, got error", err)
				}
				assert.Equal(t, expectedContents[idx], resourceData)
				idx = idx + 1
			case error:
				t.Fatal("received an error while reading ADD resource", titem)
			}
		}
	}
}

func mustBeRunCommand(t *testing.T, testClient rootfs.ClientProvider) {
	if _, ok := testClient.NextCommand().(commands.Run); !ok {
		t.Fatal("expected RUN command")
	}
}

const testDockerfileSingleStage = `FROM alpine:3.13
ARG PARAM1=value
ENV ENVPARAM1=envparam1
RUN mkdir -p /dir
ADD resource1 /target/resource1
COPY resource2 /target/resource2
RUN cp /dir/${ENVPARAM1} \
	&& call --arg=${PARAM1}`

const testDockerfileMultiStage = `FROM alpine:3.13 as builder

FROM alpine:3.13
ARG PARAM1=value
ENV ENVPARAM1=envparam1
RUN mkdir -p /dir
ADD resource1 /target/resource1
COPY resource2 /target/resource2
COPY --from=builder /etc/test /etc/test
RUN cp /dir/${ENVPARAM1} \
	&& call --arg=${PARAM1}`
