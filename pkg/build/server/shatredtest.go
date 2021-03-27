package server

import (
	"testing"

	"github.com/hashicorp/go-hclog"
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
func NewTest(t *testing.T, logger hclog.Logger, cfg *GRPCServiceConfig, ctx *WorkContext) *testGRPCServerProvider {
	return &testGRPCServerProvider{
		cfg:          cfg,
		ctx:          ctx,
		logger:       logger,
		stdErrOutput: []string{},
		stdOutOutput: []string{},
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

	chanFailed   chan error
	chanFinished chan struct{}
	chanReady    chan struct{}
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
				close(p.chanFailed)
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
				p.srv.Stop()
			case <-p.srv.OnSuccess():
				p.success = true
				p.srv.Stop()
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
