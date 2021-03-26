package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"time"

	"github.com/pkg/errors"
)

const (
	keySize = 4096
)

var (
	dnsNames = []string{"localhost"}
	//dnsNames    = []string{}
	//ipAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	ipAddresses = []net.IP{}
)

func genCert(template, parent *x509.Certificate, publicKey *rsa.PublicKey, privateKey *rsa.PrivateKey) (*x509.Certificate, []byte, error) {
	certBytes, err := x509.CreateCertificate(rand.Reader, template, parent, publicKey, privateKey)
	if err != nil {
		return nil, []byte{}, err
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, []byte{}, err
	}

	b := pem.Block{Type: "CERTIFICATE", Bytes: certBytes}
	certPEM := pem.EncodeToMemory(&b)

	return cert, certPEM, nil
}

func GenCARoot() (*x509.Certificate, []byte, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, []byte{}, nil, errors.Wrap(err, "failed generating root ca key")
	}

	var rootTemplate = x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	rootCert, rootPEM, err := genCert(&rootTemplate, &rootTemplate, &priv.PublicKey, priv)
	if err != nil {
		return nil, []byte{}, nil, err
	}
	return rootCert, rootPEM, priv, nil
}

/*
func GenRootIntermediate(RootCert *x509.Certificate, RootKey *rsa.PrivateKey) (*x509.Certificate, []byte, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, []byte{}, nil, errors.Wrap(err, "failed generiting intermediate root key")
	}

	var DCATemplate = x509.Certificate{
		SerialNumber:          big.NewInt(2),
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	DCACert, DCAPEM, err := genCert(&DCATemplate, RootCert, &priv.PublicKey, RootKey)
	if err != nil {
		return nil, []byte{}, nil, err
	}
	return DCACert, DCAPEM, priv, nil
}
*/
func GenServerCert(DCACert *x509.Certificate, DCAKey *rsa.PrivateKey) (*x509.Certificate, []byte, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		panic(err)
	}

	var ServerTemplate = x509.Certificate{
		SerialNumber: big.NewInt(3),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:         false,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}

	ServerCert, ServerPEM, err := genCert(&ServerTemplate, DCACert, &priv.PublicKey, DCAKey)
	if err != nil {
		return nil, []byte{}, nil, err
	}
	return ServerCert, ServerPEM, priv, nil
}

func GenClientCert(DCACert *x509.Certificate, DCAKey *rsa.PrivateKey) (*x509.Certificate, []byte, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		panic(err)
	}

	var ClientTemplate = x509.Certificate{
		SerialNumber: big.NewInt(4),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:         false,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}

	ClientCert, ClientPEM, err := genCert(&ClientTemplate, DCACert, &priv.PublicKey, DCAKey)
	if err != nil {
		return nil, []byte{}, nil, err
	}
	return ClientCert, ClientPEM, priv, nil
}
