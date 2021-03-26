package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
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

type GRPCServiceConfig struct {
	BindHostPort              string
	GracefulStopTimeoutMillis int
	ServerName                string
	TLSConfigServer           *tls.Config
	// TLSConfigClient contains a tls.Config to use with the client
	// but only when TLSConfigServer was not given.
	// The client config is obtained from auto-generated CA.
	// If the TLSConfigServer was provided, the client config will be always nil.
	TLSConfigClient *tls.Config
}

type ServerProvider interface {
	ServerEventProvider
	Start(serverCtx *Context)
	Stop()
	ReadyNotify() <-chan struct{}
	FailedNotify() <-chan error
	StoppedNotify() <-chan struct{}
}

type Resources = map[string][]resources.ResolvedResource
type Context struct {
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
func New(cfg *GRPCServiceConfig, logger hclog.Logger) ServerProvider {
	return &grpcSvc{
		config:      cfg,
		logger:      logger,
		chanFailed:  make(chan error, 1),
		chanReady:   make(chan struct{}),
		chanStopped: make(chan struct{}),
	}
}

func (s *grpcSvc) Start(serverCtx *Context) {
	s.Lock()
	defer s.Unlock()

	if !s.wasStarted {

		s.wasStarted = true

		listener, err := net.Listen("tcp", s.config.BindHostPort)
		if err != nil {
			//s.logger.Error("Failed to create TCP listener", "hostport", s.config.BindHostPort, "reason", err)
			s.chanFailed <- err
			return
		}

		grpcServerOptions := []grpc.ServerOption{}

		if s.config.TLSConfigServer == nil {

			//
			// auto-generate a bootstrap only root CA
			// with a server and client cert
			//

			rootCert, rootPem, rootKey, rootCaCertErr := ca.GenCARoot()
			if rootCaCertErr != nil {
				s.chanFailed <- rootCaCertErr
				return
			}

			s.logger.Info("root CA cert generated")

			//intermediateCaCert, _ /* intermediateCaPem */, intermediateCaKey, intermediateCaCertErr := ca.GenRootIntermediate(rootCert, rootKey)
			//if intermediateCaCertErr != nil {
			//	s.chanFailed <- intermediateCaCertErr
			//	return
			//}

			//s.logger.Info("root CA intermediate cert generated")

			_, serverPem, serverKey, serverCertErr := ca.GenServerCert(rootCert, rootKey)
			if serverCertErr != nil {
				s.chanFailed <- serverCertErr
				return
			}

			s.logger.Info("server cert generated")

			_, clientPem, clientKey, clientCertErr := ca.GenClientCert(rootCert, rootKey)
			if clientCertErr != nil {
				s.chanFailed <- clientCertErr
				return
			}

			rootCAs := x509.NewCertPool()
			rootCAs.AppendCertsFromPEM(rootPem)

			serverKeyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})
			serverTlsCertificate, err := tls.X509KeyPair(serverPem, serverKeyBytes)
			if err != nil {
				s.chanFailed <- clientCertErr
				return
			}

			clientKeyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})
			clientTlsCertificate, err := tls.X509KeyPair(clientPem, clientKeyBytes)
			if err != nil {
				s.chanFailed <- clientCertErr
				return
			}

			grpcServerOptions = append(grpcServerOptions, grpc.Creds(credentials.NewTLS(&tls.Config{
				ClientAuth:   tls.RequireAndVerifyClientCert,
				ClientCAs:    rootCAs,
				Certificates: []tls.Certificate{serverTlsCertificate},
				//InsecureSkipVerify: true,
			})))

			clientRootCAs := x509.NewCertPool()
			clientRootCAs.AppendCertsFromPEM(rootPem)

			s.config.TLSConfigClient = &tls.Config{
				ServerName:   s.config.ServerName,
				RootCAs:      clientRootCAs,
				Certificates: []tls.Certificate{clientTlsCertificate},
				//InsecureSkipVerify: true,
			}

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

		s.logger.Info("Registering seervice with the GRPC server")

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

func (impl *grpcSvc) OnAbort() <-chan error {
	return impl.svc.OnAbort()
}
func (impl *grpcSvc) OnStderr() <-chan string {
	return impl.svc.OnStderr()
}
func (impl *grpcSvc) OnStdout() <-chan string {
	return impl.svc.OnStdout()
}
func (impl *grpcSvc) OnSuccess() <-chan struct{} {
	return impl.svc.OnSuccess()
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
