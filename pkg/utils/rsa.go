package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

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
