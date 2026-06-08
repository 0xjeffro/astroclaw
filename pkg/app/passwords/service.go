package passwords

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"astroclaw/pkg/app/passwords/db"
	"astroclaw/pkg/crypto"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDataKeyMissing is returned when a credential operation runs against
// a scope whose data key was never provisioned. Data keys are provisioned
// when the owning entity is created:
//
//   - system scope: by migrate Lambda's seedSystemDataKey on first deploy
//   - workspace scope: by system.Service.CreateWorkspace
//   - user scope: by system.Service.CreateUser
//
// Hitting this error from a normal request flow means a provisioning step was failed.
var ErrDataKeyMissing = errors.New("data key not provisioned")

// Service stores and retrieves credentials using envelope encryption.
// Each scope (system, workspace, user) has its own data key wrapped by
// the configured KeyManager. The plaintext data keys are cached in
// memory after a single KMS Decrypt call per Lambda cold start.
//
// The Service does not lazily create data keys. Provisioning is owned
// by the lifecycle of the corresponding entity (system, workspace,
// user). Call ProvisionSystemDataKey / ProvisionWorkspaceDataKey /
// ProvisionUserDataKey from the entity creation path, ideally inside
// the same DB transaction.
//
// TODO: we haven't designed a mechanism wen delete a workspace or delete/ban a user, how to invalidate cached data keys need further duscussion.
type Service struct {
	pool       *pgxpool.Pool
	queries    *db.Queries
	keyManager crypto.KeyManager

	// All cache access is serialized by cacheMu. Lambdas usually handle
	// one request at a time per instance, so contention is negligible
	// so the simpler lock outweighs finer-grained alternatives.
	cacheMu     sync.Mutex
	systemDK    []byte
	workspaceDK map[string][]byte
	userDK      map[string][]byte
}

func NewService(pool *pgxpool.Pool, km crypto.KeyManager) *Service {
	return &Service{
		pool:        pool,
		queries:     db.New(pool),
		keyManager:  km,
		workspaceDK: make(map[string][]byte),
		userDK:      make(map[string][]byte),
	}
}

// =====================================================================
// System scope
// =====================================================================

func (svc *Service) GetSystemCredential(ctx context.Context, name string) (*Credential, error) {
	row, err := svc.queries.GetSystemCredential(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("system credential %q: %w", name, ErrNotFound)
		}
		return nil, fmt.Errorf("get system credential: %w", err)
	}
	dk, err := svc.loadSystemDataKey(ctx)
	if err != nil {
		return nil, err
	}
	value, err := crypto.DecryptAESGCM(dk, row.Nonce, row.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt system credential %q: %w", name, err)
	}
	return &Credential{
		Name:        row.Name,
		Description: row.Description,
		Value:       string(value),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (svc *Service) UpsertSystemCredential(ctx context.Context, name, description, value string) error {
	dk, err := svc.loadSystemDataKey(ctx)
	if err != nil {
		return err
	}
	nonce, ciphertext, err := crypto.EncryptAESGCM(dk, []byte(value))
	if err != nil {
		return fmt.Errorf("encrypt system credential %q: %w", name, err)
	}
	return svc.queries.UpsertSystemCredential(ctx, db.UpsertSystemCredentialParams{
		Name:        name,
		Description: description,
		Nonce:       nonce,
		Ciphertext:  ciphertext,
	})
}

func (svc *Service) DeleteSystemCredential(ctx context.Context, name string) error {
	return svc.queries.DeleteSystemCredential(ctx, name)
}

func (svc *Service) ListSystemCredentials(ctx context.Context) ([]*CredentialMetadata, error) {
	rows, err := svc.queries.ListSystemCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("list system credentials: %w", err)
	}
	out := make([]*CredentialMetadata, len(rows))
	for i, r := range rows {
		out[i] = &CredentialMetadata{
			Name:        r.Name,
			Description: r.Description,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		}
	}
	return out, nil
}

// =====================================================================
// Workspace scope
// =====================================================================

func (svc *Service) GetWorkspaceCredential(ctx context.Context, workspaceID, name string) (*Credential, error) {
	row, err := svc.queries.GetWorkspaceCredential(ctx, db.GetWorkspaceCredentialParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("workspace credential %q: %w", name, ErrNotFound)
		}
		return nil, fmt.Errorf("get workspace credential: %w", err)
	}
	dk, err := svc.loadWorkspaceDataKey(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	value, err := crypto.DecryptAESGCM(dk, row.Nonce, row.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt workspace credential %q: %w", name, err)
	}
	return &Credential{
		Name:        row.Name,
		Description: row.Description,
		Value:       string(value),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (svc *Service) UpsertWorkspaceCredential(ctx context.Context, workspaceID, name, description, value string) error {
	dk, err := svc.loadWorkspaceDataKey(ctx, workspaceID)
	if err != nil {
		return err
	}
	nonce, ciphertext, err := crypto.EncryptAESGCM(dk, []byte(value))
	if err != nil {
		return fmt.Errorf("encrypt workspace credential %q: %w", name, err)
	}
	return svc.queries.UpsertWorkspaceCredential(ctx, db.UpsertWorkspaceCredentialParams{
		WorkspaceID: workspaceID,
		Name:        name,
		Description: description,
		Nonce:       nonce,
		Ciphertext:  ciphertext,
	})
}

func (svc *Service) DeleteWorkspaceCredential(ctx context.Context, workspaceID, name string) error {
	return svc.queries.DeleteWorkspaceCredential(ctx, db.DeleteWorkspaceCredentialParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
}

func (svc *Service) ListWorkspaceCredentials(ctx context.Context, workspaceID string) ([]*CredentialMetadata, error) {
	rows, err := svc.queries.ListWorkspaceCredentials(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace credentials: %w", err)
	}
	out := make([]*CredentialMetadata, len(rows))
	for i, r := range rows {
		out[i] = &CredentialMetadata{
			Name:        r.Name,
			Description: r.Description,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		}
	}
	return out, nil
}

// =====================================================================
// User scope
// =====================================================================

func (svc *Service) GetUserCredential(ctx context.Context, userID, name string) (*Credential, error) {
	row, err := svc.queries.GetUserCredential(ctx, db.GetUserCredentialParams{
		UserID: userID,
		Name:   name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user credential %q: %w", name, ErrNotFound)
		}
		return nil, fmt.Errorf("get user credential: %w", err)
	}
	dk, err := svc.loadUserDataKey(ctx, userID)
	if err != nil {
		return nil, err
	}
	value, err := crypto.DecryptAESGCM(dk, row.Nonce, row.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt user credential %q: %w", name, err)
	}
	return &Credential{
		Name:        row.Name,
		Description: row.Description,
		Value:       string(value),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (svc *Service) UpsertUserCredential(ctx context.Context, userID, name, description, value string) error {
	dk, err := svc.loadUserDataKey(ctx, userID)
	if err != nil {
		return err
	}
	nonce, ciphertext, err := crypto.EncryptAESGCM(dk, []byte(value))
	if err != nil {
		return fmt.Errorf("encrypt user credential %q: %w", name, err)
	}
	return svc.queries.UpsertUserCredential(ctx, db.UpsertUserCredentialParams{
		UserID:      userID,
		Name:        name,
		Description: description,
		Nonce:       nonce,
		Ciphertext:  ciphertext,
	})
}

func (svc *Service) DeleteUserCredential(ctx context.Context, userID, name string) error {
	return svc.queries.DeleteUserCredential(ctx, db.DeleteUserCredentialParams{
		UserID: userID,
		Name:   name,
	})
}

func (svc *Service) ListUserCredentials(ctx context.Context, userID string) ([]*CredentialMetadata, error) {
	rows, err := svc.queries.ListUserCredentials(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list user credentials: %w", err)
	}
	out := make([]*CredentialMetadata, len(rows))
	for i, r := range rows {
		out[i] = &CredentialMetadata{
			Name:        r.Name,
			Description: r.Description,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		}
	}
	return out, nil
}

// =====================================================================
// Provisioning (called once when the owning entity is created)
// =====================================================================

// ProvisionSystemDataKey generates and wraps a data key for the system
// scope and stores it. Safe to call multiple times: if a row already
// exists, this returns nil without overwriting it.
func (svc *Service) ProvisionSystemDataKey(ctx context.Context) error {
	if _, err := svc.queries.GetSystemDataKey(ctx); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check system data key: %w", err)
	}
	dk, err := crypto.GenerateDataKey()
	if err != nil {
		return err
	}
	wrapped, err := svc.keyManager.Encrypt(ctx, dk)
	if err != nil {
		return fmt.Errorf("wrap system data key: %w", err)
	}
	return svc.queries.UpsertSystemDataKey(ctx, wrapped)
}

// ProvisionWorkspaceDataKey generates and wraps a data key for a new
// workspace. Must be called from the workspace creation path, ideally
// in the same transaction. Returns nil if the workspace already has a
// data key (idempotent).
//
// TODO: accept a *pgx.Tx so the data key insert can share the same
// transaction as the workspace insert. For now this runs on its own
// connection.
func (svc *Service) ProvisionWorkspaceDataKey(ctx context.Context, workspaceID string) error {
	if _, err := svc.queries.GetWorkspaceDataKey(ctx, workspaceID); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check workspace data key: %w", err)
	}
	dk, err := crypto.GenerateDataKey()
	if err != nil {
		return err
	}
	wrapped, err := svc.keyManager.Encrypt(ctx, dk)
	if err != nil {
		return fmt.Errorf("wrap workspace data key: %w", err)
	}
	return svc.queries.UpsertWorkspaceDataKey(ctx, db.UpsertWorkspaceDataKeyParams{
		WorkspaceID:      workspaceID,
		EncryptedDataKey: wrapped,
	})
}

// ProvisionUserDataKey generates and wraps a data key for a new user.
// Idempotent like ProvisionWorkspaceDataKey.
//
// TODO: accept a *pgx.Tx.
func (svc *Service) ProvisionUserDataKey(ctx context.Context, userID string) error {
	if _, err := svc.queries.GetUserDataKey(ctx, userID); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check user data key: %w", err)
	}
	dk, err := crypto.GenerateDataKey()
	if err != nil {
		return err
	}
	wrapped, err := svc.keyManager.Encrypt(ctx, dk)
	if err != nil {
		return fmt.Errorf("wrap user data key: %w", err)
	}
	return svc.queries.UpsertUserDataKey(ctx, db.UpsertUserDataKeyParams{
		UserID:           userID,
		EncryptedDataKey: wrapped,
	})
}

// =====================================================================
// Data key cache loaders (read-only)
// =====================================================================

// loadSystemDataKey returns the cached system data key, or decrypts the
// row from DB on first access. Returns ErrDataKeyMissing if the row does
// not exist — provisioning was skipped or rolled back.
func (svc *Service) loadSystemDataKey(ctx context.Context) ([]byte, error) {
	svc.cacheMu.Lock()
	defer svc.cacheMu.Unlock()

	if svc.systemDK != nil {
		return svc.systemDK, nil
	}
	row, err := svc.queries.GetSystemDataKey(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("system: %w", ErrDataKeyMissing)
		}
		return nil, fmt.Errorf("read system data key: %w", err)
	}
	dk, err := svc.keyManager.Decrypt(ctx, row.EncryptedDataKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt system data key: %w", err)
	}
	svc.systemDK = dk
	return dk, nil
}

func (svc *Service) loadWorkspaceDataKey(ctx context.Context, workspaceID string) ([]byte, error) {
	svc.cacheMu.Lock()
	defer svc.cacheMu.Unlock()

	if dk, ok := svc.workspaceDK[workspaceID]; ok {
		return dk, nil
	}
	row, err := svc.queries.GetWorkspaceDataKey(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("workspace %s: %w", workspaceID, ErrDataKeyMissing)
		}
		return nil, fmt.Errorf("read workspace data key: %w", err)
	}
	dk, err := svc.keyManager.Decrypt(ctx, row.EncryptedDataKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt workspace data key: %w", err)
	}
	svc.workspaceDK[workspaceID] = dk
	return dk, nil
}

func (svc *Service) loadUserDataKey(ctx context.Context, userID string) ([]byte, error) {
	svc.cacheMu.Lock()
	defer svc.cacheMu.Unlock()

	if dk, ok := svc.userDK[userID]; ok {
		return dk, nil
	}
	row, err := svc.queries.GetUserDataKey(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user %s: %w", userID, ErrDataKeyMissing)
		}
		return nil, fmt.Errorf("read user data key: %w", err)
	}
	dk, err := svc.keyManager.Decrypt(ctx, row.EncryptedDataKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt user data key: %w", err)
	}
	svc.userDK[userID] = dk
	return dk, nil
}
