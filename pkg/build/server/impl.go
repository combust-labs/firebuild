package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/combust-labs/firebuild/grpc/proto"
	"github.com/gofrs/uuid"
	"github.com/hashicorp/go-hclog"
)

// TODO: handle closing of channels when the owning server is closed.

// EventProvider provides the event subsriptions to the server executor.
// When client event occurs, a corresponding event will be sent via one of the channels.
type EventProvider interface {
	OnAbort() <-chan error
	OnStderr() <-chan string
	OnStdout() <-chan string
	OnSuccess() <-chan struct{}
}

type serverImplInterface interface {
	proto.RootfsServerServer
	EventProvider
}

type serverImpl struct {
	logger        hclog.Logger
	serviceConfig *GRPCServiceConfig
	serverCtx     *WorkContext

	chanAbort   chan error
	chanStderr  chan string
	chanStdout  chan string
	chanSuccess chan struct{}
}

func newServerImpl(logger hclog.Logger, serverCtx *WorkContext, serviceConfig *GRPCServiceConfig) serverImplInterface {
	return &serverImpl{
		logger:        logger,
		serviceConfig: serviceConfig,
		serverCtx:     serverCtx,
		chanAbort:     make(chan error, 1),
		chanStderr:    make(chan string),
		chanStdout:    make(chan string),
		chanSuccess:   make(chan struct{}),
	}
}

func (impl *serverImpl) Abort(ctx context.Context, req *proto.AbortRequest) (*proto.Empty, error) {
	impl.chanAbort <- errors.New(req.Error)
	return &proto.Empty{}, nil
}

func (impl *serverImpl) Commands(ctx context.Context, _ *proto.Empty) (*proto.CommandsResponse, error) {
	response := &proto.CommandsResponse{Command: []string{}}
	for _, cmd := range impl.serverCtx.ExecutableCommands {
		commandBytes, err := json.Marshal(cmd)
		if err != nil {
			impl.chanAbort <- err
			return response, err
		}
		response.Command = append(response.Command, string(commandBytes))
	}
	return response, nil
}

func (impl *serverImpl) Resource(req *proto.ResourceRequest, stream proto.RootfsServer_ResourceServer) error {
	if resources, ok := impl.serverCtx.ResourcesResolved[req.Path]; ok {
		// TODO: can this be parallel?
		for _, resource := range resources {

			resourceUUID := uuid.Must(uuid.NewV4()).String()
			stream.Send(&proto.ResourceChunk{
				Payload: &proto.ResourceChunk_Header{
					Header: &proto.ResourceChunk_ResourceHeader{
						SourcePath:    resource.SourcePath(),
						TargetPath:    resource.TargetPath(),
						FileMode:      int64(resource.TargetMode()),
						IsDir:         resource.IsDir(),
						TargetUser:    resource.TargetUser().Value,
						TargetWorkdir: resource.TargetWorkdir().Value,
						Id:            resourceUUID,
					},
				},
			})

			reader, err := resource.Contents()
			if err != nil {
				return err
			}

			// make it a little bit smaller than the actual max size
			safeBufSize := impl.serviceConfig.SafeClientMaxRecvMsgSize()

			impl.logger.Debug("sending data with safe buffer size", "resource", resource.TargetPath(), "safe-buffer-size", safeBufSize)

			buffer := make([]byte, safeBufSize)

			for {
				readBytes, err := reader.Read(buffer)
				if readBytes == 0 && err == io.EOF {
					stream.Send(&proto.ResourceChunk{
						Payload: &proto.ResourceChunk_Eof{
							Eof: &proto.ResourceChunk_ResourceEof{
								Id: resourceUUID,
							},
						},
					})
					break
				} else {
					payload := buffer[0:readBytes]
					hash := sha256.Sum256(payload)
					stream.Send(&proto.ResourceChunk{
						Payload: &proto.ResourceChunk_Chunk{
							Chunk: &proto.ResourceChunk_ResourceContents{
								Chunk:    payload,
								Checksum: hash[:],
								Id:       resourceUUID,
							},
						},
					})
				}
			}
		}
	} else {
		return fmt.Errorf("not found: '%s/%s'", req.Stage, req.Path)
	}
	return nil
}

func (impl *serverImpl) StdErr(ctx context.Context, req *proto.LogMessage) (*proto.Empty, error) {
	for _, line := range req.Line {
		impl.chanStdout <- line
	}
	return nil, nil
}

func (impl *serverImpl) StdOut(ctx context.Context, req *proto.LogMessage) (*proto.Empty, error) {
	for _, line := range req.Line {
		impl.chanStdout <- line
	}
	return nil, nil
}

func (impl *serverImpl) Success(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	close(impl.chanSuccess)
	return nil, nil
}

func (impl *serverImpl) OnAbort() <-chan error {
	return impl.chanAbort
}
func (impl *serverImpl) OnStderr() <-chan string {
	return impl.chanStderr
}
func (impl *serverImpl) OnStdout() <-chan string {
	return impl.chanStdout
}
func (impl *serverImpl) OnSuccess() <-chan struct{} {
	return impl.chanSuccess
}
