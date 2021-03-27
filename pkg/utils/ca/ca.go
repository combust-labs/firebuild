package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"
)

// TODO: Move this package out into a module
// under Apache 2.0 license.

// CertificateData contains a x509.Certificate and PEM bytes.
type CertificateData interface {
	// Returns a certificate.
	Certificate() *x509.Certificate
	// Returns PEM bytes.
	PEM() []byte
}

type defaultCertificateData struct {
	cert *x509.Certificate
	pem  []byte
}

func (d *defaultCertificateData) Certificate() *x509.Certificate {
	return d.cert
}
func (d *defaultCertificateData) PEM() []byte {
	return d.pem
}

// EmbeddedCAConfig is the embedded CA configuration.
type EmbeddedCAConfig struct {
	Addresses       []string
	CertsValidFor   time.Duration
	KeySize         int
	UseIntermediate bool
}

// WithDefaultsApplied applies defaults to any not set values.
func (c *EmbeddedCAConfig) WithDefaultsApplied() *EmbeddedCAConfig {
	if c.Addresses == nil {
		c.Addresses = []string{"localhost"}
	}
	if c.CertsValidFor == 0 {
		c.CertsValidFor = time.Hour
	}
	if c.KeySize == 0 {
		c.KeySize = 4096
	}
	return c
}

// EmbeddedCA represents an embedded bootstrap time only CA.
type EmbeddedCA interface {
	// Creates a new client certificate and constructs a tls.Config valid for this CA.
	NewClientCertTLSConfig(string) (*tls.Config, error)
	// Creates a new server certificate and constructs a tls.Config valid for this CA.
	NewServerCertTLSConfig() (*tls.Config, error)
}

type defaultEmbeddedCA struct {
	m                *sync.Mutex
	config           *EmbeddedCAConfig
	nextSerialNumber *big.Int

	logger hclog.Logger

	chain *x509.CertPool

	rootCertificateData CertificateData
	rootKey             *rsa.PrivateKey

	signingCertificateData CertificateData
	signingKey             *rsa.PrivateKey
}

// NewDefaultEmbeddedCA creates and initializes a new instance of an embedded CA with a default logger.
func NewDefaultEmbeddedCA(config *EmbeddedCAConfig) (EmbeddedCA, error) {
	return NewDefaultEmbeddedCAWithLogger(config, hclog.Default())
}

// NewDefaultEmbeddedCAWithLogger creates and initializes a new instance of an embedded CA with a provided logger.
func NewDefaultEmbeddedCAWithLogger(config *EmbeddedCAConfig, logger hclog.Logger) (EmbeddedCA, error) {
	eca := &defaultEmbeddedCA{
		m:                &sync.Mutex{},
		config:           config.WithDefaultsApplied(),
		nextSerialNumber: big.NewInt(0),
		logger:           logger,
		chain:            x509.NewCertPool(),
	}
	return eca.initCA()
}

func (eca *defaultEmbeddedCA) genCert(template, parent *x509.Certificate, publicKey *rsa.PublicKey, privateKey *rsa.PrivateKey) (CertificateData, error) {
	certBytes, err := x509.CreateCertificate(rand.Reader, template, parent, publicKey, privateKey)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, err
	}
	b := pem.Block{Type: "CERTIFICATE", Bytes: certBytes}
	certPEM := pem.EncodeToMemory(&b)
	return &defaultCertificateData{cert: cert, pem: certPEM}, nil
}

// NewClientCertTLSConfig creates a new client certificate and constructs a tls.Config valid for this CA.
func (eca *defaultEmbeddedCA) NewClientCertTLSConfig(serverNameOverride string) (*tls.Config, error) {
	priv, err := eca.getNewKey()
	if err != nil {
		return nil, errors.Wrap(err, "failed generating client key")
	}
	clientCertData, err := eca.genCert(eca.getCertTemplate(),
		eca.signingCertificateData.Certificate(),
		&priv.PublicKey, eca.signingKey)
	if err != nil {
		return nil, err
	}
	clientKeyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	clientTLSCertificate, err := tls.X509KeyPair(clientCertData.PEM(), clientKeyBytes)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		ServerName:   serverNameOverride,
		RootCAs:      eca.chain,
		Certificates: []tls.Certificate{clientTLSCertificate},
	}, nil
}

// NewServerCertTLSConfig creates a new server certificate and constructs a tls.Config valid for this CA.
func (eca *defaultEmbeddedCA) NewServerCertTLSConfig() (*tls.Config, error) {
	priv, err := eca.getNewKey()
	if err != nil {
		return nil, errors.Wrap(err, "failed generating server key")
	}
	serverCertData, err := eca.genCert(eca.getCertTemplate(),
		eca.signingCertificateData.Certificate(),
		&priv.PublicKey, eca.signingKey)
	if err != nil {
		return nil, err
	}
	serverKeyBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	serverTLSCertificate, err := tls.X509KeyPair(serverCertData.PEM(), serverKeyBytes)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    eca.chain,
		Certificates: []tls.Certificate{serverTLSCertificate},
	}, nil
}

func (eca *defaultEmbeddedCA) applyIPAndSAN(template *x509.Certificate) *x509.Certificate {
	for _, address := range eca.config.Addresses {
		if ip := net.ParseIP(address); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, address)
		}
	}
	return template
}

func (eca *defaultEmbeddedCA) getCertTemplate() *x509.Certificate {
	return eca.applyIPAndSAN(&x509.Certificate{
		SerialNumber: eca.nextSerial(),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(eca.config.CertsValidFor),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:         false,
	})
}

func (eca *defaultEmbeddedCA) getIntermediateCACertTemplate() *x509.Certificate {
	return eca.applyIPAndSAN(&x509.Certificate{
		SerialNumber:          eca.nextSerial(),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(eca.config.CertsValidFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	})
}

func (eca *defaultEmbeddedCA) getNewKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, eca.config.KeySize)
}

func (eca *defaultEmbeddedCA) getRootCACertTemplate() *x509.Certificate {
	return eca.applyIPAndSAN(&x509.Certificate{
		SerialNumber:          eca.nextSerial(),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(eca.config.CertsValidFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen: func() int {
			if eca.config.UseIntermediate {
				return 2
			}
			return 1
		}(),
		/*
			ExtraExtensions: []pkix.Extension{{
				Id:       asn1.ObjectIdentifier{2, 5, 29, 17},
				Critical: false,
				Value:    []byte(`email:in...@example.com, URI:http://ca.example.com/`),
			}},
		*/
	})
}

func (eca *defaultEmbeddedCA) initCA() (EmbeddedCA, error) {

	rootKey, err := eca.getNewKey()
	if err != nil {
		return nil, errors.Wrap(err, "failed generating root ca key")
	}

	rootCertTemplate := eca.getRootCACertTemplate()

	rootCertData, err := eca.genCert(rootCertTemplate, rootCertTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}

	eca.logger.Debug("root certificate generated")

	eca.rootCertificateData = rootCertData
	eca.rootKey = rootKey
	eca.signingCertificateData = rootCertData
	eca.signingKey = rootKey
	eca.chain.AppendCertsFromPEM(rootCertData.PEM())

	eca.logger.Debug("root certificate appended to the pool, signer set to root certificate")

	if eca.config.UseIntermediate {

		intermediateKey, err := eca.getNewKey()
		if err != nil {
			return nil, errors.Wrap(err, "failed generating intermediate ca key")
		}
		intermediateCertData, err := eca.genCert(eca.getIntermediateCACertTemplate(),
			eca.signingCertificateData.Certificate(),
			&intermediateKey.PublicKey,
			eca.signingKey)
		if err != nil {
			return nil, err
		}

		eca.logger.Debug("intermediate certificate generated")

		eca.signingCertificateData = intermediateCertData
		eca.signingKey = intermediateKey
		eca.chain.AppendCertsFromPEM(intermediateCertData.PEM())

		eca.logger.Debug("intermediate certificate appended to the pool, signer updated to intermediate")
	}

	return eca, nil
}

func (eca *defaultEmbeddedCA) nextSerial() *big.Int {
	eca.m.Lock()
	defer eca.m.Unlock()
	return eca.nextSerialNumber.Add(eca.nextSerialNumber, big.NewInt(1))
}
