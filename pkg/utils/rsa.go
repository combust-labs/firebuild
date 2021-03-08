package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
)

// Defaults
const (
	RSABitSize = 4096
)

// EncodePrivateKeyToPEM encodes Private Key from RSA to PEM format.
func EncodePrivateKeyToPEM(privateKey *rsa.PrivateKey) []byte {
	privDER := x509.MarshalPKCS1PrivateKey(privateKey)
	// pem.Block
	privBlock := pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: nil,
		Bytes:   privDER,
	}
	// Private key in PEM format
	return pem.EncodeToMemory(&privBlock)
}

// GenerateRSAPrivateKey generates a new RSA private key.
func GenerateRSAPrivateKey(bitSize int) (*rsa.PrivateKey, error) {
	// Private Key generation
	privateKey, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		return nil, err
	}
	// Validate Private Key
	err = privateKey.Validate()
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}

// GetSSHKey generates an SSH public key for the given private key.
func GetSSHKey(privatekey *rsa.PrivateKey) (ssh.PublicKey, error) {
	publicRsaKey, err := ssh.NewPublicKey(&privatekey.PublicKey)
	if err != nil {
		return nil, err
	}
	return publicRsaKey, nil
}

// MarshalSSHPublicKey marshals SSH public key to the OpenSSH format so it can be used for authorized_keys file.
func MarshalSSHPublicKey(key ssh.PublicKey) []byte {
	return ssh.MarshalAuthorizedKey(key)
}

// SSHPublicKeyFromBytes reads an SSH public key from bytes.
func SSHPublicKeyFromBytes(b []byte) (ssh.PublicKey, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey(b)
	return key, err
}

// SSHPublicKeyFromFile reads an SSH public key from a PEM file.
func SSHPublicKeyFromFile(path string) (ssh.PublicKey, error) {
	pemStat, statErr := os.Stat(path)
	if statErr != nil {
		return nil, errors.Wrap(statErr, "failed looking up the SSH key file")
	}
	if !pemStat.Mode().IsRegular() {
		return nil, fmt.Errorf("SSH key file path must point to a regular file")
	}
	pemFileBytes, readErr := ioutil.ReadFile(path)
	if readErr != nil {
		return nil, errors.Wrap(readErr, "failed reading the SSH key file")
	}
	key, _, _, _, parseErr := ssh.ParseAuthorizedKey(pemFileBytes)
	return key, parseErr
}
