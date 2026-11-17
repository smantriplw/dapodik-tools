// Package secret provides authenticated encryption for credentials stored at
// rest, such as the admin-managed Dapodik cookie and webservice token.
package secret

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

const aad = "smanti-credentials-v1"

type Box struct {
	aead cipher.AEAD
}

func New(keyMaterial string) (*Box, error) {
	keyMaterial = strings.TrimSpace(keyMaterial)
	if keyMaterial == "" {
		return nil, errors.New("key material kredensial kosong")
	}
	sum := sha256.Sum256([]byte(aad + "|" + keyMaterial))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Encrypt(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := b.aead.Seal(nil, nonce, []byte(plain), []byte(aad))
	out := make([]byte, 0, len(nonce)+len(ciphertext))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func (b *Box) Decrypt(encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("membaca kredensial: %w", err)
	}
	nonceSize := b.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("kredensial tidak valid (terlalu pendek)")
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plain, err := b.aead.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return "", errors.New("kredensial tidak dapat didekripsi; key berubah atau data rusak")
	}
	return string(plain), nil
}
