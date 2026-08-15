package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestCipherRoundTripBindsCiphertextToSettingKey(t *testing.T) {
	t.Parallel()

	cipher, err := NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	encrypted, err := cipher.Encrypt("openai.api_key", "test-sensitive-value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if strings.Contains(encrypted, "test-sensitive-value") {
		t.Fatal("ciphertext contains plaintext")
	}

	plaintext, err := cipher.Decrypt("openai.api_key", encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "test-sensitive-value" {
		t.Fatalf("Decrypt() = %q", plaintext)
	}
	if _, err := cipher.Decrypt("discord.webhook_url", encrypted); err == nil {
		t.Fatal("Decrypt() with a different setting key succeeded")
	}
}

func TestNewCipherRejectsNon256BitKey(t *testing.T) {
	t.Parallel()

	if _, err := NewCipher([]byte("too-short")); err == nil {
		t.Fatal("NewCipher() accepted a non-256-bit key")
	}
}
