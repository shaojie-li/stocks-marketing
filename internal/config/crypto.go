package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const ciphertextVersion byte = 1
const ciphertextPrefix = "v1:"

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("settings master key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create settings cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create settings AEAD: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(key, plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate settings nonce: %w", err)
	}
	payload := append([]byte{ciphertextVersion}, nonce...)
	payload = c.aead.Seal(payload, nonce, []byte(plaintext), []byte(key))
	return ciphertextPrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (c *Cipher) Decrypt(key, encoded string) (string, error) {
	if !strings.HasPrefix(encoded, ciphertextPrefix) {
		return "", errors.New("unsupported encrypted setting payload")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, ciphertextPrefix))
	if err != nil {
		return "", errors.New("decode encrypted setting")
	}
	nonceSize := c.aead.NonceSize()
	if len(payload) < 1+nonceSize || payload[0] != ciphertextVersion {
		return "", errors.New("unsupported encrypted setting payload")
	}
	nonce := payload[1 : 1+nonceSize]
	plaintext, err := c.aead.Open(nil, nonce, payload[1+nonceSize:], []byte(key))
	if err != nil {
		return "", errors.New("authenticate encrypted setting")
	}
	return string(plaintext), nil
}
