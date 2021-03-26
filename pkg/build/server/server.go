package server

import (
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/combust-labs/firebuild/grpc/proto"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/combust-labs/firebuild/pkg/utils/ca"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// GRPCServiceConfig contains the configuration for the GRPC server.
type GRPCServiceConfig struct {
	// Host and port to bind on
	BindHostPort string
	// How long to wait for the GRPC server to shutdown
	// before stopping forcefully.
	GracefulStopTimeoutMillis int
	// Identifies the GRPC server. This setting is required when doing mTLS.
	ServerName string
	// Contains the GRPC server configuration.
	// If not provided, a runtime, build only CA and TLS context will be created.
	TLSConfigServer *tls.Config
	// TLSConfigClient contains a tls.Config to use with the client
	// but only when TLSConfigServer was not given.
	// The client config is obtained from auto-generated CA.
	// If the TLSConfigServer was provided, the client config will be always nil.
	TLSConfigClient *tls.Config
}

// Provider defines a GRPC server behaviour.
type Provider interface {
	EventProvider
	// Starts the server with a given work context.
	Start(serverCtx *WorkContext)
	// Stops the server, if the server is started.
	Stop()
	// ReadyNotify returns a channel that will be closed when the server is ready to serve client requests.
	ReadyNotify() <-chan struct{}
	// FailedNotify returns a channel that will be contain the error if the server has failed to start.
	FailedNotify() <-chan error
	// StoppedNotify returns a channel that will be closed when the server has stopped.
	StoppedNotify() <-chan struct{}
}

// Resources is a map of resolved resources the server handles for the client.
type Resources = map[string][]resources.ResolvedResource

// WorkContext contains the information for the bootstrap work to execute.
type WorkContext struct {
	ExecutableCommands []interface{}
	ResourcesResolved  Resources
}

type grpcSvc struct {
	sync.Mutex

	config *GRPCServiceConfig
	logger hclog.Logger

	srv *grpc.Server
	svc serverImplInterface

	chanReady   chan struct{}
	chanStopped chan struct{}
	chanFailed  chan error

	wasStarted bool
	running    bool
}

// New returns a new instance of the server.
func New(cfg *GRPCServiceConfig, logger hclog.Logger) Provider {
	return &grpcSvc{
		config:      cfg,
		logger:      logger,
		chanFailed:  make(chan error, 1),
		chanReady:   make(chan struct{}),
		chanStopped: make(chan struct{}),
	}
}

// Start starts the server with a given work context.
func (s *grpcSvc) Start(serverCtx *WorkContext) {
	s.Lock()
	defer s.Unlock()

	if !s.wasStarted {
		s.wasStarted = true
		listener, err := net.Listen("tcp", s.config.BindHostPort)
		if err != nil {
			s.chanFailed <- err
			return
		}

		grpcServerOptions := []grpc.ServerOption{}

		if s.config.TLSConfigServer == nil {

			// if there is no server TLS config, generate a new runtime CA
			// and create a new server and client TLS config

			embeddedCA, embeddedCAErr := ca.NewDefaultEmbeddedCAWithLogger(&ca.EmbeddedCAConfig{}, s.logger.Named("embdedded-ca"))
			if embeddedCAErr != nil {
				s.chanFailed <- embeddedCAErr
				return
			}

			serverTLSConfig, err := embeddedCA.NewServerCertTLSConfig()
			if err != nil {
				s.chanFailed <- embeddedCAErr
				return
			}

			clientTLSConfig, err := embeddedCA.NewClientCertTLSConfig(s.config.ServerName)
			if err != nil {
				s.chanFailed <- embeddedCAErr
				return
			}

			grpcServerOptions = append(grpcServerOptions, grpc.Creds(credentials.NewTLS(serverTLSConfig)))

			s.config.TLSConfigClient = clientTLSConfig

		} else {
			grpcServerOptions = append(grpcServerOptions, grpc.Creds(credentials.NewTLS(s.config.TLSConfigServer)))
		}

		s.srv = grpc.NewServer(grpcServerOptions...)

		/*
			if !s.config.NoTLS {

				s.logger.Info("Starting with TLS")

				certificate, err := tls.LoadX509KeyPair(s.config.TLSCertificateFilePath, s.config.TLSKeyFilePath)
				if err != nil {
					s.logger.Error("Failed to load server certificate or key",
						"cert-file-path", s.config.TLSCertificateFilePath,
						"key-file-path", s.config.TLSKeyFilePath,
						"reason", err)
					s.chanFailed <- err
					return
				}

				tlsConfig := &tls.Config{
					Certificates: []tls.Certificate{certificate},
				}

				if s.config.TLSTrustedCertificatesFilePath != "" {
					certPool := x509.NewCertPool()
					ca, err := ioutil.ReadFile(s.config.TLSTrustedCertificatesFilePath)
					if err != nil {
						s.logger.Error("Failed to load trusted certificate",
							"trusted-cert-file-path", s.config.TLSTrustedCertificatesFilePath,
							"reason", err)
						s.chanFailed <- err
						return
					}
					if ok := certPool.AppendCertsFromPEM(ca); !ok {
						s.logger.Error("Failed to append trusted certificate to the cert pool", "reason", err)
						s.chanFailed <- err
						return
					}
					tlsConfig.ClientCAs = certPool
					tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
				}

				creds := credentials.NewTLS(tlsConfig)
				s.srv = grpc.NewServer(grpc.Creds(creds))
			} else {

				s.logger.Warn("Starting without TLS, use TLS in production")
				s.srv = grpc.NewServer()

			}

		*/

		s.logger.Info("Registering service with the GRPC server")

		s.svc = newServerImpl(serverCtx)

		proto.RegisterRootfsServerServer(s.srv, s.svc)

		chanErr := make(chan struct{})
		go func() {
			if err := s.srv.Serve(listener); err != nil {
				s.logger.Error("Failed to serve", "reason", "error")
				s.chanFailed <- err
				close(chanErr)
			}
		}()

		select {
		case <-chanErr:
		case <-time.After(100):
			s.logger.Info("GRPC server running")
			s.running = true
			s.config.BindHostPort = listener.Addr().String()
			close(s.chanReady)
		}

	} else {
		s.logger.Warn("Server was already started, can't start twice")
	}
}

// Stop stops the server, if the server is started.
func (s *grpcSvc) Stop() {

	s.Lock()
	defer s.Unlock()

	if s.running {

		s.logger.Info("attempting graceful stop")

		chanSignal := make(chan struct{})
		go func() {
			s.srv.GracefulStop()
			close(chanSignal)
		}()

		select {
		case <-chanSignal:
			s.logger.Info("stopped gracefully")
		case <-time.After(time.Millisecond * time.Duration(s.config.GracefulStopTimeoutMillis)):
			s.logger.Warn("failed to stop gracefully within timeout, forceful stop")
			s.srv.Stop()
		}

		s.logger.Info("stopped")

		s.running = false
		close(s.chanStopped)

	} else {
		s.logger.Warn("server not running")
	}

}

func (s *grpcSvc) OnAbort() <-chan error {
	return s.svc.OnAbort()
}
func (s *grpcSvc) OnStderr() <-chan string {
	return s.svc.OnStderr()
}
func (s *grpcSvc) OnStdout() <-chan string {
	return s.svc.OnStdout()
}
func (s *grpcSvc) OnSuccess() <-chan struct{} {
	return s.svc.OnSuccess()
}

// ReadyNotify returns a channel that will be closed when the server is ready to serve client requests.
func (s *grpcSvc) ReadyNotify() <-chan struct{} {
	return s.chanReady
}

// FailedNotify returns a channel that will be contain the error if the server has failed to start.
func (s *grpcSvc) FailedNotify() <-chan error {
	return s.chanFailed
}

// StoppedNotify returns a channel that will be closed when the server has stopped.
func (s *grpcSvc) StoppedNotify() <-chan struct{} {
	return s.chanStopped
}
