package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/combust-labs/firebuild/grpc/proto"
	"github.com/gofrs/uuid"
	"github.com/hashicorp/go-hclog"
)

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
	Stop()
}

type serverImpl struct {
	m       *sync.Mutex
	stopped bool

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
		m:             &sync.Mutex{},
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
	impl.m.Lock()
	if impl.stopped {
		defer impl.m.Unlock()
		return &proto.Empty{}, nil
	}
	impl.m.Unlock()

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
	if ress, ok := impl.serverCtx.ResourcesResolved[req.Path]; ok {
		for _, resource := range ress {

			reader, err := resource.Contents()
			if err != nil {
				return err
			}

			// make it a little bit smaller than the actual max size
			safeBufSize := impl.serviceConfig.SafeClientMaxRecvMsgSize()

			impl.logger.Debug("sending data with safe buffer size", "resource", resource.TargetPath(), "safe-buffer-size", safeBufSize)

			if resource.IsDir() {
				grpcDirResource := NewGRPCDirectoryResource(safeBufSize, resource)
				outputChannel := grpcDirResource.WalkResource()
				for {
					payload := <-outputChannel
					if payload == nil {
						break
					}
					stream.Send(payload)
				}
				continue
			}

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
	impl.m.Lock()
	if impl.stopped {
		defer impl.m.Unlock()
		return &proto.Empty{}, nil
	}
	impl.m.Unlock()

	for _, line := range req.Line {
		impl.chanStderr <- line
	}
	return &proto.Empty{}, nil
}

func (impl *serverImpl) StdOut(ctx context.Context, req *proto.LogMessage) (*proto.Empty, error) {
	impl.m.Lock()
	if impl.stopped {
		defer impl.m.Unlock()
		return &proto.Empty{}, nil
	}
	impl.m.Unlock()

	for _, line := range req.Line {
		impl.chanStdout <- line
	}
	return &proto.Empty{}, nil
}

func (impl *serverImpl) Stop() {
	impl.m.Lock()
	if impl.stopped {
		impl.m.Unlock()
		return
	}
	impl.stopped = true
	impl.m.Unlock()
}

func (impl *serverImpl) Success(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	impl.m.Lock()
	if impl.stopped {
		defer impl.m.Unlock()
		return &proto.Empty{}, nil
	}
	impl.m.Unlock()

	close(impl.chanSuccess)
	return &proto.Empty{}, nil
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
