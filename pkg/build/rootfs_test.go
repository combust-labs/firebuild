package build

import (
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/combust-labs/firebuild/grpc/proto"
	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/combust-labs/firebuild/pkg/build/server"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func mustNewArg(t *testing.T, rawValue string) commands.Arg {
	arg, err := commands.NewRawArg(rawValue)
	if err != nil {
		t.Fatal(err)
	}
	return arg
}

func TestContextBuilderSingleStageWithResources(t *testing.T) {

	logger := hclog.Default()
	logger.SetLevel(hclog.Debug)

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
	buildCtx, err := contextBuilder.WithResolver(resources.NewDefaultResolver()).CreateContext(make(server.Resources))
	if err != nil {
		t.Fatal("expected build context to be created, got error", err)
	}

	grpcConfig := &server.GRPCServiceConfig{
		BindHostPort: "127.0.0.1:5000",
	}

	srv := server.New(grpcConfig, logger.Named("grpc-server"))
	srv.Start(buildCtx)

	select {
	case startErr := <-srv.FailedNotify():
		t.Fatal("expected the GRPC server to start but it failed", startErr)
	case <-srv.ReadyNotify():
		t.Log("GRPC server started and serving on", grpcConfig.BindHostPort)
		defer srv.Stop()
	}

	grpcConn, err := grpc.Dial(grpcConfig.BindHostPort,
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(grpcConfig.MaxRecvMsgSize)),
		grpc.WithTransportCredentials(credentials.NewTLS(grpcConfig.TLSConfigClient)))

	if err != nil {
		t.Fatal("expected GRPC connection to dial, go error", err)
	}
	grpcClient := proto.NewRootfsServerClient(grpcConn)

	response, err := grpcClient.Commands(context.Background(), &proto.Empty{})
	if err != nil {
		t.Fatal("expected GRPC client Commands() to return, go error", err)
	}

	for _, cmd := range response.Command {
		t.Log("Command", cmd)
	}

	for _, resourcePath := range []string{"resource1", "resource2"} {

		resourceClient, err := grpcClient.Resource(context.Background(), &proto.ResourceRequest{
			Path: resourcePath,
		})

		if err != nil {
			t.Fatal("expected resource to be read from GRPC, got error", err)
		}

		for {
			response, err := resourceClient.Recv()

			if response == nil {
				resourceClient.CloseSend()
				break
			}

			// yes, err check after response check
			if err != nil {
				t.Fatal("failed reading chunk from server, got error", err)
			}

			switch tresponse := response.GetPayload().(type) {
			case *proto.ResourceChunk_Eof:
				t.Log(" ===============> finished file", tresponse.Eof.Id, "for", resourcePath)
			case *proto.ResourceChunk_Chunk:
				t.Log(" ===============> received chunk", tresponse.Chunk.Id,
					"chunk", string(tresponse.Chunk.Chunk),
					"checksum", string(tresponse.Chunk.Checksum))
			case *proto.ResourceChunk_Header:
				t.Log(" ===============> new file", tresponse.Header.SourcePath,
					"target", tresponse.Header.TargetPath,
					"mode", fs.FileMode(tresponse.Header.FileMode),
					"isDir", tresponse.Header.IsDir,
					"user", tresponse.Header.TargetUser,
					"workdir", tresponse.Header.TargetWorkdir)
			}
		}

	}
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
ENV ENVPARAM1=envparam1
RUN mkdir -p /dir
ADD resource1 /target/resource1
COPY resource2 /target/resource2
RUN cp /dir/${ENVPARAM1} \
	&& call --arg=${PARAM1}`
