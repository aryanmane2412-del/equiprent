// Package fieldcrypt encrypts sensitive identifiers (PAN, licence and bank
// account numbers) before they reach the database, with AES-256-GCM, and
// derives keyed lookup hashes (HMAC-SHA256) for duplicate detection without
// decrypting. Only the last four characters are ever shown.
package fieldcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Crypter encrypts and hashes field values.
type Crypter struct {
	aead    cipher.AEAD
	hashKey []byte
}

// New builds a Crypter from a base64 32-byte key and a hash key.
func New(b64Key, hashKey string) (*Crypter, error) {
	key, err := base64.StdEncoding.DecodeString(b64Key)
	if err != nil || len(key) != 32 {
		return nil, errors.New("fieldcrypt: key must be 32 bytes, base64-encoded")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if hashKey == "" {
		return nil, errors.New("fieldcrypt: hash key required")
	}
	return &Crypter{aead: aead, hashKey: []byte(hashKey)}, nil
}

// Normalize upper-cases and strips spaces and dashes (PAN, IFSC, numbers).
func Normalize(v string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(v)))
}

// Encrypt returns nonce||ciphertext. The field name is bound as associated
// data, so a PAN ciphertext cannot be swapped into a bank-account column.
func (c *Crypter) Encrypt(field, plain string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plain), []byte(field)), nil
}

// Decrypt reverses Encrypt.
func (c *Crypter) Decrypt(field string, data []byte) (string, error) {
	n := c.aead.NonceSize()
	if len(data) < n {
		return "", errors.New("fieldcrypt: ciphertext too short")
	}
	plain, err := c.aead.Open(nil, data[:n], data[n:], []byte(field))
	if err != nil {
		return "", fmt.Errorf("fieldcrypt: decrypt %s: %w", field, err)
	}
	return string(plain), nil
}

// Hash is a keyed, deterministic lookup hash of a normalized value.
func (c *Crypter) Hash(field, value string) []byte {
	m := hmac.New(sha256.New, c.hashKey)
	m.Write([]byte(field + "|" + Normalize(value)))
	return m.Sum(nil)
}

// Last4 returns the last four characters of the normalized value.
func Last4(v string) string {
	n := Normalize(v)
	if len(n) <= 4 {
		return n
	}
	return n[len(n)-4:]
}

// Sealed is an encrypted field ready for storage.
type Sealed struct {
	Enc   []byte
	Hash  []byte
	Last4 string
}

// Seal normalizes, encrypts and hashes a value in one step.
func (c *Crypter) Seal(field, value string) (Sealed, error) {
	n := Normalize(value)
	enc, err := c.Encrypt(field, n)
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{Enc: enc, Hash: c.Hash(field, n), Last4: Last4(n)}, nil
}
