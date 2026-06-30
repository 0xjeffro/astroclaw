package crypto

import (
	"context"
	"fmt"

	"gocloud.dev/secrets"

	// Driver registrations. Each blank import wires its URL scheme into
	// secrets.DefaultMux so OpenKeyManager can dispatch by URL.
	_ "gocloud.dev/secrets/awskms"
	_ "gocloud.dev/secrets/localsecrets"
)

// urlKeyManager adapts a gocloud.dev secrets.Keeper to KeyManager.
type urlKeyManager struct {
	keeper *secrets.Keeper
}

// OpenKeyManager returns a KeyManager backed by gocloud.dev/secrets, picked
// by URL scheme. Supported URLs:
//
//	awskms://<keyID>?region=<region> AWS KMS, keyID is UUID, alias, or ARN
//	base64key://<base64-master-key> Local AES-GCM, for tests and demos
//
// More drivers (gcpkms, azurekeyvault, hashivault, ...) can be enabled by
// adding their blank imports to this file.
func OpenKeyManager(ctx context.Context, url string) (KeyManager, error) {
	keeper, err := secrets.OpenKeeper(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open keeper %q: %w", url, err)
	}
	return &urlKeyManager{keeper: keeper}, nil
}

func (k *urlKeyManager) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	return k.keeper.Encrypt(ctx, plaintext)
}

func (k *urlKeyManager) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	return k.keeper.Decrypt(ctx, ciphertext)
}
