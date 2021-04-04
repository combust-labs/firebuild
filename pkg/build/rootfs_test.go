package build

import (
	"fmt"
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

	rootfs.MustPutTestResource(t, dockerfilePath, []byte(testDockerfileMultiStage))
	rootfs.MustPutTestResource(t, filepath.Join(tempDir, "resource1"), []byte(expectedResource1Bytes))
	rootfs.MustPutTestResource(t, filepath.Join(tempDir, "resource2"), []byte(expectedResource2Bytes))

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

		rootfs.MustPutTestResource(tt, filepath.Join(tempDir, "etc/test/file1"), []byte("etc/test/file1"))
		rootfs.MustPutTestResource(tt, filepath.Join(tempDir, "etc/test/file2"), []byte("etc/test/file2"))
		rootfs.MustPutTestResource(tt, filepath.Join(tempDir, "etc/test/subdir/subdir-file1"), []byte("etc/test/subdir/subdir-file1"))

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

		rootfs.MustBeRunCommand(tt, testClient)
		rootfs.MustBeAddCommand(tt, testClient, expectedResource1Bytes)
		rootfs.MustBeCopyCommand(tt, testClient, expectedResource2Bytes)
		// directories do not have a byte content, they always return empty bytes:
		// since we have:
		// - /etc/test: dir
		// - /etc/test/file1: file
		// - /etc/test/file2: file
		// - /etc/test/subdir: dir
		// - /etc/test/subdir/subdir-file1: file
		// we expect the following:
		rootfs.MustBeCopyCommand(tt, testClient, []byte{}, []byte("etc/test/file1"), []byte("etc/test/file2"), []byte{}, []byte("etc/test/subdir/subdir-file1"))
		rootfs.MustBeRunCommand(tt, testClient)
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

	rootfs.MustPutTestResource(t, dockerfilePath, []byte(testDockerfileSingleStage))
	rootfs.MustPutTestResource(t, filepath.Join(tempDir, "resource1"), []byte(expectedResource1Bytes))
	rootfs.MustPutTestResource(t, filepath.Join(tempDir, "resource2"), []byte(expectedResource2Bytes))

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

	rootfs.MustBeRunCommand(t, testClient)
	rootfs.MustBeAddCommand(t, testClient, expectedResource1Bytes)
	rootfs.MustBeCopyCommand(t, testClient, expectedResource2Bytes)
	rootfs.MustBeRunCommand(t, testClient)
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
