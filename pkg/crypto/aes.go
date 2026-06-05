package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// dataKeySize is the AES key length in bytes. 32 bytes = AES-256.
const dataKeySize = 32

// GenerateDataKey returns a fresh 32-byte AES-256 key drawn from crypto/rand.
// Use this when a workspace/user/system scope first needs to encrypt
// something: keep the returned key in memory for local AES-GCM operations
// and pass it to KeyManager.Encrypt to obtain the wrapped form for storage.
func GenerateDataKey() ([]byte, error) {
	key := make([]byte, dataKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	return key, nil
}

// EncryptAESGCM encrypts plaintext with AES-256-GCM using the given data key.
// It returns a fresh 12-byte nonce and the ciphertext. The caller stores
// both alongside the row, since DecryptAESGCM needs both back.
//
// A new nonce is generated for every call.
func EncryptAESGCM(dataKey, plaintext []byte) (nonce, ciphertext []byte, err error) {
	gcm, err := newGCM(dataKey)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// DecryptAESGCM reverses EncryptAESGCM. It returns the plaintext, or a
// non-nil error if the ciphertext or nonce was modified after encryption
// (AES-GCM checks an authentication tag stored at the end of the ciphertext).
func DecryptAESGCM(dataKey, nonce, ciphertext []byte) ([]byte, error) {
	gcm, err := newGCM(dataKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("aes-gcm open: %w", err)
	}
	return plaintext, nil
}

// newGCM constructs a GCM cipher around an AES block cipher keyed with
// dataKey. It is shared by EncryptAESGCM and DecryptAESGCM.
func newGCM(dataKey []byte) (cipher.AEAD, error) {
	if len(dataKey) != dataKeySize {
		return nil, fmt.Errorf("data key must be %d bytes, got %d", dataKeySize, len(dataKey))
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return gcm, nil
}
