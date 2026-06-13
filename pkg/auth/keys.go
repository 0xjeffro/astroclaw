package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"

	"astroclaw/pkg/app/passwords"
)

// SecretLoader fetches the JWT signing secret from the passwords app on
// first use and caches the decoded bytes for the lifetime of the process.
// Cold starts pay a single DB + KMS round trip; warm invocations hit the
// in-memory cache only. Failures are not cached, so a transient DB or KMS
// blip during cold start does not poison the instance.
//
// SecretLoader is safe for concurrent use.
type SecretLoader struct {
	pwSvc *passwords.Service

	mu    sync.Mutex
	cache []byte
}

func NewSecretLoader(pwSvc *passwords.Service) *SecretLoader {
	return &SecretLoader{pwSvc: pwSvc}
}

// Get returns the decoded secret. The first successful call loads from DB
// and decodes base64; later calls return the cached bytes. If load fails,
// the next call retries.
func (l *SecretLoader) Get(ctx context.Context) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cache != nil {
		return l.cache, nil
	}
	decoded, err := l.load(ctx)
	if err != nil {
		return nil, err
	}
	l.cache = decoded
	return l.cache, nil
}

func (l *SecretLoader) load(ctx context.Context) ([]byte, error) {
	cred, err := l.pwSvc.GetSystemCredential(ctx, passwords.SystemCredJWTSecret)
	if err != nil {
		return nil, fmt.Errorf("load %s credential: %w", passwords.SystemCredJWTSecret, err)
	}
	decoded, err := base64.RawStdEncoding.DecodeString(cred.Value)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", passwords.SystemCredJWTSecret, err)
	}
	return decoded, nil
}
