package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Vault provides AES-256-GCM encryption/decryption with a passphrase-derived key.
type Vault struct {
	key [32]byte
}

// New creates a Vault by deriving an AES-256 key from the passphrase via Argon2id.
// The salt is deterministic (SHA-256 of passphrase), so the same passphrase always
// produces the same key across restarts.
func New(passphrase string) *Vault {
	salt := sha256.Sum256([]byte(passphrase))
	key := argon2.IDKey([]byte(passphrase), salt[:16], 1, 64*1024, 4, 32)

	v := &Vault{}
	copy(v.key[:], key)
	return v
}

// Encrypt encrypts plaintext using AES-256-GCM with a random nonce.
func (v *Vault) Encrypt(plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create gcm: %w", err)
	}

	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Decrypt decrypts ciphertext using AES-256-GCM with the provided nonce.
func (v *Vault) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}
