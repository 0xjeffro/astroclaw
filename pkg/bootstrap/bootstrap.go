// Package bootstrap runs the database migrations and idempotent seed
// steps that put an empty astroclaw database into a usable initial
// state (admin user, JWT signing secret, default workspace, genesis
// agent, bundled skills).
//
// It is shared across deploy targets. AWS calls it from the migrate
// Lambda; docker calls it from a one-shot init container. All
// deploy-specific glue (env parsing, CloudFormation event handling,
// DSQL vs Postgres pool construction) stays in the caller.
package bootstrap

import (
	"context"
	"fmt"

	"astroclaw/pkg/app/agents"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	appskills "astroclaw/pkg/app/skills"
	"astroclaw/pkg/app/system"
	"astroclaw/pkg/crypto"

	"github.com/jackc/pgx/v5/pgxpool"
	"gocloud.dev/blob"
)

// Config drives Run. Pool, KeyManager, and Bucket are always required;
// the rest are optional and default to sensible values or "skip".
type Config struct {
	Pool       *pgxpool.Pool
	KeyManager crypto.KeyManager
	Bucket     *blob.Bucket

	// DSQLMode toggles DSQL-specific SQL adjustments (CREATE INDEX
	// ASYNC, one-DDL-per-transaction). Set to true only on Aurora
	// DSQL, false for vanilla Postgres.
	DSQLMode bool

	// MigrationsDir is where *.sql migration files live. Default
	// "migrations" (relative to the process working directory).
	MigrationsDir string

	// SkillsDir is where @author/name skill directories live. Default
	// "skills" (relative to the process working directory).
	SkillsDir string

	// AnthropicAPIKey seeds the system-scope Anthropic credential when
	// non-empty. Empty leaves any existing credential untouched.
	AnthropicAPIKey string

	// AdminPassword sets (or resets) the admin user's password when
	// non-empty. Empty leaves the existing password untouched. The
	// caller is expected to generate and surface this to the operator
	// out-of-band.
	AdminPassword string
}

// Run applies pending migrations and seeds the baseline records.
// Safe to call repeatedly; every step is idempotent.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	cfg.applyDefaults()

	if err := RunMigrations(ctx, cfg.Pool, cfg.DSQLMode, cfg.MigrationsDir); err != nil {
		return err
	}
	return Seed(ctx, cfg)
}

// Seed executes only the seed steps against an already-migrated
// database. The migrate step is skipped; callers who ran migrations
// out-of-band (e.g. via `atlas migrate apply`) can use this.
func Seed(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	cfg.applyDefaults()

	pwSvc := passwords.NewService(cfg.Pool, cfg.KeyManager)
	sysSvc := system.NewService(cfg.Pool, system.WithProvisioner(pwSvc))
	agentsSvc := agents.NewService(cfg.Pool)
	settingsSvc := settings.NewService(cfg.Pool)
	skillsSvc := appskills.NewService(cfg.Pool)

	// Order matters:
	//   - system data key must exist before any system credential is written
	//   - passwords service must be injected into system.Service so
	//     CreateUser and CreateWorkspace can provision per-entity data keys
	if err := seedSystemDataKey(ctx, pwSvc); err != nil {
		return err
	}
	if err := seedCredentials(ctx, pwSvc, cfg.AnthropicAPIKey); err != nil {
		return err
	}
	if err := seedJWTSecret(ctx, pwSvc); err != nil {
		return err
	}
	if err := seedDefaultUser(ctx, sysSvc, cfg.AdminPassword); err != nil {
		return err
	}
	if err := seedDefaultWorkspace(ctx, sysSvc); err != nil {
		return err
	}
	if err := seedDefaultAgent(ctx, sysSvc, agentsSvc, settingsSvc); err != nil {
		return err
	}
	if err := seedDefaultSkills(ctx, sysSvc, skillsSvc, cfg.Bucket, cfg.SkillsDir); err != nil {
		return err
	}
	return nil
}

func (c *Config) validate() error {
	if c.Pool == nil {
		return fmt.Errorf("bootstrap: Pool is required")
	}
	if c.KeyManager == nil {
		return fmt.Errorf("bootstrap: KeyManager is required")
	}
	if c.Bucket == nil {
		return fmt.Errorf("bootstrap: Bucket is required")
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.MigrationsDir == "" {
		c.MigrationsDir = "migrations"
	}
	if c.SkillsDir == "" {
		c.SkillsDir = "skills"
	}
}
