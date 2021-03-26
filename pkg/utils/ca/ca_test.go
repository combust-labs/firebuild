package ca

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
)

func TestEmbeddedCAWithIntermediate(t *testing.T) {

	caConfig := &EmbeddedCAConfig{
		Addresses:       []string{"example.com", "localhost"},
		CertsValidFor:   time.Minute,
		KeySize:         1024,
		UseIntermediate: true,
	}
	embeddedCA, err := NewDefaultEmbeddedCAWithLogger(caConfig, hclog.Default())
	if err != nil {
		t.Fatal("expected embedded CA to be initialized, got error", err)
	}

	serverTLSConfig, err := embeddedCA.NewServerCertTLSConfig()
	if err != nil {
		t.Fatal("expected server TLS config to be created, got error", err)
	}

	assert.Equal(t, tls.RequireAndVerifyClientCert, serverTLSConfig.ClientAuth)
	assert.Equal(t, 1, len(serverTLSConfig.Certificates))
	assert.Equal(t, 2, len(serverTLSConfig.ClientCAs.Subjects()))

	clientTLSConfig, err := embeddedCA.NewClientCertTLSConfig("test-server")
	if err != nil {
		t.Fatal("expected server TLS config to be created, got error", err)
	}

	assert.Equal(t, "test-server", clientTLSConfig.ServerName)
	assert.Equal(t, 1, len(clientTLSConfig.Certificates))
	assert.Equal(t, 2, len(clientTLSConfig.RootCAs.Subjects()))

}

func TestEmbeddedCAWithoutIntermediate(t *testing.T) {

	caConfig := &EmbeddedCAConfig{
		Addresses:     []string{"example.com", "localhost"},
		CertsValidFor: time.Minute,
		KeySize:       1024,
	}
	embeddedCA, err := NewDefaultEmbeddedCAWithLogger(caConfig, hclog.Default())
	if err != nil {
		t.Fatal("expected embedded CA to be initialized, got error", err)
	}

	serverTLSConfig, err := embeddedCA.NewServerCertTLSConfig()
	if err != nil {
		t.Fatal("expected server TLS config to be created, got error", err)
	}

	assert.Equal(t, tls.RequireAndVerifyClientCert, serverTLSConfig.ClientAuth)
	assert.Equal(t, 1, len(serverTLSConfig.Certificates))
	assert.Equal(t, 1, len(serverTLSConfig.ClientCAs.Subjects()))

	clientTLSConfig, err := embeddedCA.NewClientCertTLSConfig("test-server")
	if err != nil {
		t.Fatal("expected server TLS config to be created, got error", err)
	}

	assert.Equal(t, "test-server", clientTLSConfig.ServerName)
	assert.Equal(t, 1, len(clientTLSConfig.Certificates))
	assert.Equal(t, 1, len(clientTLSConfig.RootCAs.Subjects()))

}
