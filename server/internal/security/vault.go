package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrInvalidMasterKey  = errors.New("master encryption key must be 32 bytes (64 hex chars or 32-byte base64)")
	ErrInvalidCiphertext = errors.New("invalid or tampered ciphertext")
)

type Vault struct {
	key []byte
}

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, ErrInvalidMasterKey
	}
	copied := make([]byte, 32)
	copy(copied, key)
	return &Vault{key: copied}, nil
}

func NewVaultFromEnv(raw string) (*Vault, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ErrInvalidMasterKey
	}
	if len(trimmed) == 64 {
		if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) == 32 {
			return NewVault(decoded)
		}
	}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(decoded) == 32 {
		return NewVault(decoded)
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil && len(decoded) == 32 {
		return NewVault(decoded)
	}
	return nil, ErrInvalidMasterKey
}

func (v *Vault) Encrypt(plaintext string) ([]byte, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return ciphertext, nil
}

func (v *Vault) Decrypt(blob []byte) (string, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(blob) < nonceSize {
		return "", ErrInvalidCiphertext
	}
	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	return string(plaintext), nil
}

func MaskLast4(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}
