package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/combust-labs/firebuild/grpc/proto"
)

type ServerEventProvider interface {
	OnAbort() <-chan error
	OnStderr() <-chan string
	OnStdout() <-chan string
	OnSuccess() <-chan struct{}
}

type serverImplInterface interface {
	proto.RootfsServerServer
	ServerEventProvider
}

type serverImpl struct {
	serverCtx *Context

	chanAbort   chan error
	chanStderr  chan string
	chanStdout  chan string
	chanSuccess chan struct{}
}

func newServerImpl(serverCtx *Context) serverImplInterface {
	return &serverImpl{
		serverCtx:   serverCtx,
		chanAbort:   make(chan error, 1),
		chanStderr:  make(chan string),
		chanStdout:  make(chan string),
		chanSuccess: make(chan struct{}),
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
	return errors.New("not implemented")
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
