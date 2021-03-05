package utils

import (
	"crypto/rand"
	"crypto/rsa"

	"golang.org/x/crypto/ssh"
)

// Defaults
const (
	RSABitSize = 4096
)

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

// GenerateRSAPublicKey generates an RSA public key for the given private key
func GenerateRSAPublicKey(privatekey *rsa.PublicKey) (ssh.PublicKey, error) {
	publicRsaKey, err := ssh.NewPublicKey(privatekey)
	if err != nil {
		return nil, err
	}
	return publicRsaKey, nil
}

// MarshalSSHPublicKey marshals SSH public key to the OpenSSH format so it can be used for authorized_keys file.
func MarshalSSHPublicKey(key ssh.PublicKey) []byte {
	return ssh.MarshalAuthorizedKey(key)
}
