package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := GenerateDataKey()
	if err != nil {
		t.Fatalf("GenerateDataKey: %v", err)
	}
	plaintext := []byte("sk-ant-very-secret-token")

	nonce, ciphertext, err := EncryptAESGCM(key, plaintext)
	if err != nil {
		t.Fatalf("EncryptAESGCM: %v", err)
	}

	got, err := DecryptAESGCM(key, nonce, ciphertext)
	if err != nil {
		t.Fatalf("DecryptAESGCM: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestDecryptFailsOnTamperedCiphertext(t *testing.T) {
	key, _ := GenerateDataKey()
	nonce, ciphertext, _ := EncryptAESGCM(key, []byte("hello"))

	// Flip one bit in the ciphertext. The GCM authentication tag should catch it.
	ciphertext[0] ^= 0x01

	if _, err := DecryptAESGCM(key, nonce, ciphertext); err == nil {
		t.Fatal("expected error when ciphertext is tampered, got nil")
	}
}

func TestEachEncryptGeneratesUniqueNonce(t *testing.T) {
	key, _ := GenerateDataKey()
	plaintext := []byte("same input")

	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		nonce, _, err := EncryptAESGCM(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptAESGCM iteration %d: %v", i, err)
		}
		if _, dup := seen[string(nonce)]; dup {
			t.Fatalf("duplicate nonce produced at iteration %d", i)
		}
		seen[string(nonce)] = struct{}{}
	}
}

func TestRejectsWrongKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33, 64} {
		shortKey := make([]byte, n)
		if _, _, err := EncryptAESGCM(shortKey, []byte("x")); err == nil {
			t.Errorf("EncryptAESGCM accepted invalid key length %d, want error", n)
		}
	}
}
