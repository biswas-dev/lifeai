// Package secret encrypts third-party credentials at rest.
//
// AES-256-GCM keyed from ENCRYPTION_KEY. The key is hashed to 32 bytes so any
// reasonably long passphrase works; a random nonce per value means the same
// token stored twice does not produce the same ciphertext.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// ErrNoKey means no ENCRYPTION_KEY is configured.
var ErrNoKey = errors.New("secret: no encryption key configured")

// Cipher seals and opens small strings.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a passphrase. An empty passphrase yields a nil
// Cipher, whose methods return ErrNoKey — so callers can carry one around
// without checking at every use.
func New(passphrase string) (*Cipher, error) {
	if passphrase == "" {
		return nil, nil
	}
	sum := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Enabled reports whether values can be sealed.
func (c *Cipher) Enabled() bool { return c != nil && c.aead != nil }

// Seal encrypts plaintext to a base64 string.
func (c *Cipher) Seal(plaintext string) (string, error) {
	if !c.Enabled() {
		return "", ErrNoKey
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal.
func (c *Cipher) Open(sealed string) (string, error) {
	if !c.Enabled() {
		return "", ErrNoKey
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secret: ciphertext too short")
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
