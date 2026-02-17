package vault

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	v := New("test-passphrase")
	plaintext := []byte("hello, vault!")

	ciphertext, nonce, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := v.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("got %q, want %q", decrypted, plaintext)
	}
}

func TestWrongPassphrase(t *testing.T) {
	v1 := New("correct-passphrase")
	v2 := New("wrong-passphrase")

	ciphertext, nonce, err := v1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = v2.Decrypt(ciphertext, nonce)
	if err == nil {
		t.Fatal("expected error decrypting with wrong passphrase")
	}
}

func TestDifferentPassphrasesDifferentKeys(t *testing.T) {
	v1 := New("passphrase-one")
	v2 := New("passphrase-two")

	if v1.key == v2.key {
		t.Fatal("different passphrases produced the same key")
	}
}

func TestEmptyPlaintext(t *testing.T) {
	v := New("test")

	ciphertext, nonce, err := v.Encrypt([]byte{})
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}

	decrypted, err := v.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}

	if len(decrypted) != 0 {
		t.Fatalf("expected empty, got %d bytes", len(decrypted))
	}
}
