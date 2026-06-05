package crypto

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/kms"
)

// AWSKMSKeyManager implements KeyManager backed by AWS KMS.
// Encrypt calls kms:Encrypt with the configured key. Decrypt calls
// kms:Decrypt and lets KMS pick the right key version from the
// ciphertext metadata, so automatic key rotation is transparent.
type AWSKMSKeyManager struct {
	client *kms.Client
	keyID  string // CMK ARN or alias (e.g. "alias/astroclaw-passwords")
}

// NewAWSKMSKeyManager wires up a KeyManager that uses the given KMS client
// and customer-managed key. keyID can be the key ARN, key ID, or an alias.
func NewAWSKMSKeyManager(client *kms.Client, keyID string) *AWSKMSKeyManager {
	return &AWSKMSKeyManager{client: client, keyID: keyID}
}

func (k *AWSKMSKeyManager) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	out, err := k.client.Encrypt(ctx, &kms.EncryptInput{
		KeyId:     &k.keyID,
		Plaintext: plaintext,
	})
	if err != nil {
		return nil, fmt.Errorf("kms encrypt: %w", err)
	}
	return out.CiphertextBlob, nil
}

// Decrypt does not require keyID. AWS KMS reads the key version from the
// ciphertext blob produced by Encrypt and selects the matching key
// material automatically.
func (k *AWSKMSKeyManager) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	out, err := k.client.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: ciphertext,
	})
	if err != nil {
		return nil, fmt.Errorf("kms decrypt: %w", err)
	}
	return out.Plaintext, nil
}
