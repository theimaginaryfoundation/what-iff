package datastore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	TokenEncryptionSecretEnvVar = "TOKEN_ENCRYPTION_SECRET"
	tokenCiphertextPrefix       = "mcpv1:"
	MinTokenEncryptionSecretLen = 32
)

var errTokenEncryptionSecretNotConfigured = errors.New("token encryption secret is not configured")
var errTokenEncryptionSecretIsReference = errors.New("token encryption secret must be the secret value, not an ARN or reference")
var errTokenEncryptionSecretTooShort = fmt.Errorf("token encryption secret must be at least %d characters", MinTokenEncryptionSecretLen)

type tokenCrypto struct {
	aead cipher.AEAD
}

func newTokenCrypto(secret string) (*tokenCrypto, error) {
	trimmed, err := ValidateTokenEncryptionSecret(secret)
	if err != nil {
		return nil, err
	}

	key := sha256.Sum256([]byte(trimmed))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to initialize aes cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize aes-gcm: %w", err)
	}

	return &tokenCrypto{aead: aead}, nil
}

// ValidateTokenEncryptionSecret enforces the expected TOKEN_ENCRYPTION_SECRET constraints.
func ValidateTokenEncryptionSecret(secret string) (string, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return "", errTokenEncryptionSecretNotConfigured
	}
	if strings.HasPrefix(trimmed, "arn:") {
		return "", errTokenEncryptionSecretIsReference
	}
	if len(trimmed) < MinTokenEncryptionSecretLen {
		return "", errTokenEncryptionSecretTooShort
	}
	return trimmed, nil
}

func (t *tokenCrypto) Encrypt(plaintext string) (string, error) {
	if t == nil {
		return "", errTokenEncryptionSecretNotConfigured
	}

	nonce := make([]byte, t.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := t.aead.Seal(nil, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(append(nonce, ciphertext...))
	return tokenCiphertextPrefix + encoded, nil
}

func (t *tokenCrypto) Decrypt(ciphertext string) (string, error) {
	if t == nil {
		return "", errTokenEncryptionSecretNotConfigured
	}

	if !strings.HasPrefix(ciphertext, tokenCiphertextPrefix) {
		return "", errors.New("token ciphertext has invalid format")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, tokenCiphertextPrefix))
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceSize := t.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("token ciphertext is too short")
	}

	nonce := raw[:nonceSize]
	payload := raw[nonceSize:]
	plaintext, err := t.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt ciphertext: %w", err)
	}

	return string(plaintext), nil
}
