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
// Lambda cold starts pay a single DB + KMS round trip; warm invocations
// hit the in-memory cache only.
//
// SecretLoader is safe for concurrent use.
type SecretLoader struct {
	pwSvc *passwords.Service

	once  sync.Once
	cache []byte
	err   error
}

func NewSecretLoader(pwSvc *passwords.Service) *SecretLoader {
	return &SecretLoader{pwSvc: pwSvc}
}

// Get returns the decoded secret. The first call loads from DB and decodes
// base64; later calls return the cached bytes.
//
// Note: if the first load fails, the error is cached too. A Lambda whose
// cold-start load fails is effectively dead until it's recycled, which
// matches the current operational model. If we ever want hot recovery the
// fix is to replace sync.Once with an atomic.Pointer plus retry.
func (l *SecretLoader) Get(ctx context.Context) ([]byte, error) {
	l.once.Do(func() {
		l.cache, l.err = l.load(ctx)
	})
	return l.cache, l.err
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
	if len(decoded) < 32 {
		return nil, fmt.Errorf("%s too short: %d bytes, need >=32", passwords.SystemCredJWTSecret, len(decoded))
	}
	return decoded, nil
}
