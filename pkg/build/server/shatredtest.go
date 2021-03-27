package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/combust-labs/firebuild/grpc/proto"
	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestingServerProvider wraps an instance of a server and provides testing
// utilities around it.
type TestingServerProvider interface {
	Start()
	Stop()
	FailedNotify() <-chan error
	FinishedNotify() <-chan struct{}
	ReadyNotify() <-chan struct{}

	Aborted() error
	ConsumedStderr() []string
	ConsumedStdout() []string
	Succeeded() bool
}

// NewTest starts a new test server provider.
func NewTestServer(t *testing.T, logger hclog.Logger, cfg *GRPCServiceConfig, ctx *WorkContext) *testGRPCServerProvider {
	return &testGRPCServerProvider{
		cfg:          cfg,
		ctx:          ctx,
		logger:       logger,
		stdErrOutput: []string{},
		stdOutOutput: []string{},
		chanAborted:  make(chan struct{}),
		chanFailed:   make(chan error, 1),
		chanFinished: make(chan struct{}),
		chanReady:    make(chan struct{}),
	}
}

type testGRPCServerProvider struct {
	cfg *GRPCServiceConfig
	ctx *WorkContext
	srv Provider

	logger hclog.Logger

	abortError   error
	stdErrOutput []string
	stdOutOutput []string
	success      bool

	chanAborted  chan struct{}
	chanFailed   chan error
	chanFinished chan struct{}
	chanReady    chan struct{}

	isAbortedClosed bool
}

// Start starts a testing server.
func (p *testGRPCServerProvider) Start() {
	p.srv = New(p.cfg, p.logger)
	p.srv.Start(p.ctx)

	select {
	case <-p.srv.ReadyNotify():
		close(p.chanReady)
	case err := <-p.srv.FailedNotify():
		p.chanFailed <- err
		return
	}

	go func() {
	out:
		for {
			select {
			case <-p.srv.StoppedNotify():
				close(p.chanFinished)
				break out
			case stdErrLine := <-p.srv.OnStderr():
				if stdErrLine == "" {
					continue
				}
				p.stdErrOutput = append(p.stdErrOutput, stdErrLine)
			case stdOutLine := <-p.srv.OnStdout():
				if stdOutLine == "" {
					continue
				}
				p.stdOutOutput = append(p.stdOutOutput, stdOutLine)
			case outErr := <-p.srv.OnAbort():
				p.abortError = outErr
				close(p.chanAborted)
			case <-p.srv.OnSuccess():
				p.success = true
				go func() {
					p.srv.Stop()
				}()
			case <-p.chanAborted:
				if p.isAbortedClosed {
					continue
				}
				p.isAbortedClosed = true
				go func() {
					p.srv.Stop()
				}()
			}
		}
	}()
}

// Stop stops a testing server.
func (p *testGRPCServerProvider) Stop() {
	if p.srv != nil {
		p.srv.Stop()
	}
}

// FailedNotify returns a channel which will contain an error if the testing server failed to start.
func (p *testGRPCServerProvider) FailedNotify() <-chan error {
	return p.chanFailed
}

// FinishedNotify returns a channel which will be closed when the server is stopped.
func (p *testGRPCServerProvider) FinishedNotify() <-chan struct{} {
	return p.chanFinished
}

// ReadyNotify returns a channel which will be closed when the server is ready.
func (p *testGRPCServerProvider) ReadyNotify() <-chan struct{} {
	return p.chanReady
}

func (p *testGRPCServerProvider) Aborted() error {
	return p.abortError
}
func (p *testGRPCServerProvider) ConsumedStderr() []string {
	return p.stdErrOutput
}
func (p *testGRPCServerProvider) ConsumedStdout() []string {
	return p.stdOutOutput
}
func (p *testGRPCServerProvider) Succeeded() bool {
	return p.success
}

// -- test client

type TestClient interface {
	Abort(error) error
	Commands(*testing.T) error
	NextCommand() commands.VMInitSerializableCommand
	Resource(string) (<-chan resources.ResolvedResource, error)
	StdErr([]string) error
	StdOut([]string) error
	Success() error
}

func NewTestClient(t *testing.T, logger hclog.Logger, cfg *GRPCServiceConfig) (TestClient, error) {
	grpcConn, err := grpc.Dial(cfg.BindHostPort,
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(cfg.MaxRecvMsgSize)),
		grpc.WithTransportCredentials(credentials.NewTLS(cfg.TLSConfigClient)))

	if err != nil {
		return nil, err
	}

	return &testClient{underlying: proto.NewRootfsServerClient(grpcConn)}, nil
}

type testClient struct {
	underlying      proto.RootfsServerClient
	fetchedCommands []commands.VMInitSerializableCommand
}

func (c *testClient) Commands(t *testing.T) error {
	c.fetchedCommands = []commands.VMInitSerializableCommand{}
	response, err := c.underlying.Commands(context.Background(), &proto.Empty{})
	if err != nil {
		return err
	}
	for _, cmd := range response.Command {
		rawItem := map[string]interface{}{}
		if err := json.Unmarshal([]byte(cmd), &rawItem); err != nil {
			return err
		}

		if originalCommandString, ok := rawItem["OriginalCommand"]; ok {
			if strings.HasPrefix(fmt.Sprintf("%s", originalCommandString), "ADD") {
				command := commands.Add{}
				if err := mapstructure.Decode(rawItem, &command); err != nil {
					return errors.Wrap(err, "found ADD but did not deserialize")
				}
				c.fetchedCommands = append(c.fetchedCommands, command)
			} else if strings.HasPrefix(fmt.Sprintf("%s", originalCommandString), "COPY") {
				command := commands.Copy{}
				if err := mapstructure.Decode(rawItem, &command); err != nil {
					return errors.Wrap(err, "found COPY but did not deserialize")
				}
				c.fetchedCommands = append(c.fetchedCommands, command)
			} else if strings.HasPrefix(fmt.Sprintf("%s", originalCommandString), "RUN") {
				command := commands.Run{}
				if err := mapstructure.Decode(rawItem, &command); err != nil {
					return errors.Wrap(err, "found RUN but did not deserialize")
				}
				c.fetchedCommands = append(c.fetchedCommands, command)
			} else {
				t.Log("unexpected command from grpc:", rawItem)
			}
		}
	}
	return nil
}

func (c *testClient) NextCommand() commands.VMInitSerializableCommand {
	if len(c.fetchedCommands) == 0 {
		return nil
	}
	result := c.fetchedCommands[0]
	if len(c.fetchedCommands) == 1 {
		c.fetchedCommands = []commands.VMInitSerializableCommand{}
	} else {
		c.fetchedCommands = c.fetchedCommands[1:]
	}
	return result
}

func (c *testClient) Resource(input string) (<-chan resources.ResolvedResource, error) {

	chanResources := make(chan resources.ResolvedResource)

	resourceClient, err := c.underlying.Resource(context.Background(), &proto.ResourceRequest{Path: input})
	if err != nil {
		return nil, err
	}

	go func() {

		var currentResource *testResolvedResource

		for {
			response, err := resourceClient.Recv()

			if response == nil {
				resourceClient.CloseSend()
				break
			}

			// yes, err check after response check
			if err != nil {
				//t.Fatal("failed reading chunk from server, got error", err)
				continue
			}

			switch tresponse := response.GetPayload().(type) {
			case *proto.ResourceChunk_Eof:
				chanResources <- currentResource
			case *proto.ResourceChunk_Chunk:
				// TODO: check the checksum of the chunk...
				currentResource.contents = append(currentResource.contents, tresponse.Chunk.Chunk...)
			case *proto.ResourceChunk_Header:
				currentResource = &testResolvedResource{
					contents:      []byte{},
					isDir:         tresponse.Header.IsDir,
					sourcePath:    tresponse.Header.SourcePath,
					targetMode:    fs.FileMode(tresponse.Header.FileMode),
					targetPath:    tresponse.Header.TargetPath,
					targetUser:    tresponse.Header.TargetUser,
					targetWorkdir: tresponse.Header.TargetWorkdir,
				}
			}
		}

		close(chanResources)

	}()

	return chanResources, nil
}

func (c *testClient) StdErr(input []string) error {
	_, err := c.underlying.StdErr(context.Background(), &proto.LogMessage{Line: input})
	return err
}
func (c *testClient) StdOut(input []string) error {
	_, err := c.underlying.StdOut(context.Background(), &proto.LogMessage{Line: input})
	return err
}
func (c *testClient) Abort(input error) error {
	_, err := c.underlying.Abort(context.Background(), &proto.AbortRequest{Error: input.Error()})
	return err
}
func (c *testClient) Success() error {
	_, err := c.underlying.Success(context.Background(), &proto.Empty{})
	return err
}

// --
// test resolved resource

type testResolvedResource struct {
	contents      []byte
	isDir         bool
	sourcePath    string
	targetMode    fs.FileMode
	targetPath    string
	targetUser    string
	targetWorkdir string
}

type bytesReaderCloser struct {
	bytesReader *bytes.Reader
}

func (b *bytesReaderCloser) Close() error {
	return nil
}

func (b *bytesReaderCloser) Read(p []byte) (n int, err error) {
	return b.bytesReader.Read(p)
}

func (r *testResolvedResource) Contents() (io.ReadCloser, error) {
	return &bytesReaderCloser{bytesReader: bytes.NewReader(r.contents)}, nil
}

func (r *testResolvedResource) IsDir() bool {
	return r.isDir
}

func (r *testResolvedResource) ResolvedURIOrPath() string {
	return fmt.Sprintf("grpc://%s", r.sourcePath)
}

func (r *testResolvedResource) SourcePath() string {
	return r.sourcePath
}
func (drr *testResolvedResource) TargetMode() fs.FileMode {
	return drr.targetMode
}
func (r *testResolvedResource) TargetPath() string {
	return r.targetPath
}
func (r *testResolvedResource) TargetWorkdir() commands.Workdir {
	return commands.Workdir{Value: r.targetWorkdir}
}
func (r *testResolvedResource) TargetUser() commands.User {
	return commands.User{Value: r.targetUser}
}
