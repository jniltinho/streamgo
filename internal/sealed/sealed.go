// Package sealed encrypts the secrets StreamGo keeps in its database (destination addresses
// and keys, invite tokens) with XChaCha20-Poly1305 under STREAMGO_SECRET_KEY (design D8).
//
// Portions adapted from MeetHub (MIT): internal/sealed/sealed.go
package sealed

import (
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// ErrSealed is a sealed secret that does not open with the key (another key, or tampered data).
var ErrSealed = errors.New("the stored secret cannot be decrypted")

// Box seals and opens secrets under one key.
type Box struct{ aead cipher.AEAD }

// New returns a Box for the 32-byte key (config.Secret.Bytes()).
func New(key []byte) (*Box, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain with a random nonce, bound as associated data (what the secret belongs
// to); the result is nonce || ciphertext.
func (b *Box) Seal(bound string, plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize(), b.aead.NonceSize()+len(plain)+b.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, []byte(bound)), nil
}

// Open decrypts what Seal made for the same key and bound; anything else is ErrSealed.
func (b *Box) Open(bound string, sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrSealed
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], []byte(bound))
	if err != nil {
		return nil, ErrSealed
	}
	return plain, nil
}
