// Package crypto provides cloud-agnostic primitives for envelope encryption.
// It exposes a KeyManager interface that encrypts and decrypts small inputs
// using a managed master key (e.g. AWS KMS), and AES-GCM helpers for local
// encryption of larger data using a key returned by GenerateDataKey.
//
// Typical usage (envelope pattern):
//
//  1. data_key := crypto.GenerateDataKey()
//  2. encrypted_dk := keyManager.Encrypt(ctx, data_key)
//  3. nonce, ct := crypto.EncryptAESGCM(data_key, plaintext)
//  4. ... store encrypted_dk + nonce + ct in DB ...
//  5. data_key := keyManager.Decrypt(ctx, encrypted_dk)
//  6. plaintext := crypto.DecryptAESGCM(data_key, nonce, ct)
package crypto

import "context"

// KeyManager encrypts and decrypts byte slices using a master key that is
// managed by a cloud provider (e.g. AWS KMS) and never leaves it. We only
// use it on small inputs like the 32-byte AES data keys produced by
// GenerateDataKey, not on credential values themselves. Credential values
// are encrypted locally with AES-GCM using the unwrapped data key.
//
// Implementations are expected to be safe for concurrent use.
type KeyManager interface {
	// Encrypt encrypts plaintext with the master key.
	// Returns an opaque blob that must be passed back to Decrypt unchanged.
	Encrypt(ctx context.Context, plaintext []byte) ([]byte, error)

	// Decrypt reverses Encrypt for a blob produced by the same master key.
	// Returns an error if the blob was tampered with or was produced by a
	// different key the caller has no access to.
	Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error)
}
