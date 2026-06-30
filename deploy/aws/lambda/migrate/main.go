// Migration Runner, Technical Trade-offs
//
//  1. Background
//     In this proj we use Atlas to generate migration files from schema.sql diffs. Meanwhile, we adopt
//     Aurora DSQL as the default database for AWS deployment， it is fully serverless, zero cold-start,
//     pay-per-request, much cheaper than an always-on PostgreSQL instance for early adopters.
//     However, DSQL has some incompatibilities with standard PostgreSQL
//     (e.g., no JSONB columns, no foreign keys, one DDL per transaction).
//
//  2. Trade-offs
//     Although Atlas Pro supports DSQL-native migration generation, a paid dependency
//     is a barrier for open-source contributors. So migration generation targets
//     standard PostgreSQL (atlas migrate diff --env local), keeping the toolchain
//     free and portable. DSQL-specific adaptations (per-statement execution,
//     "already exists" detection, retry logic) live here in this Lambda, scoped
//     to the AWS deployment layer. Migration files stay database-agnostic,
//     DSQL-specific handling stays in deployment code.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"astroclaw/pkg/app/agents"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	appskills "astroclaw/pkg/app/skills"
	"astroclaw/pkg/app/system"
	"astroclaw/pkg/crypto"
	"astroclaw/pkg/storage"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event and Result follow the CloudFormation Custom Resource protocol.
// CDK's Provider framework invokes this Lambda on stack Create, Update, and Delete.
//   - Create/Update: run unapplied migrations (tracked per-statement).
//   - Delete: skip migrations and return immediately.
//
// https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/crpg-ref-requests.html
// https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.custom_resources.Provider.html
type Event struct {
	RequestType        string `json:"RequestType"`        // "Create" | "Update" | "Delete"
	PhysicalResourceID string `json:"PhysicalResourceId"` // CloudFormation's identifier for this resource
}

type Result struct {
	PhysicalResourceID string `json:"PhysicalResourceId"`
}

func handler(ctx context.Context, event Event) (*Result, error) {
	if event.RequestType == "Delete" {
		return &Result{PhysicalResourceID: event.PhysicalResourceID}, nil
	}

	// Connect to DSQL using IAM authentication via the official Go connector.
	// DSQL_ENDPOINT is set by CDK from the cluster's endpoint attribute.
	// TODO: support standard PostgreSQL via pgxpool.New() for users who choose Aurora PostgreSQL over DSQL in CDK config.
	// Go connector docs: https://docs.aws.amazon.com/aurora-dsql/latest/userguide/SECTION_program-with-go-pgx-connector.html
	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to DSQL: %w", err)
	}
	defer pool.Close()

	if err := runMigrations(ctx, pool, true); err != nil {
		return nil, err
	}

	// Wire the service graph once, then hand it to each seed step. Order
	// matters: the system data key must exist before any system credential
	// is written, and the passwords service must be injected into
	// system.Service so CreateUser and CreateWorkspace can provision
	// per-entity data keys.
	km, err := crypto.OpenKeyManager(ctx, os.Getenv("KMS_URL"))
	if err != nil {
		return nil, fmt.Errorf("open key manager: %w", err)
	}
	bucket, err := storage.OpenBucket(ctx, os.Getenv("STORAGE_URL"))
	if err != nil {
		return nil, fmt.Errorf("open storage bucket: %w", err)
	}
	defer bucket.Close()

	pwSvc := passwords.NewService(pool, km)
	sysSvc := system.NewService(pool, system.WithProvisioner(pwSvc))
	agentsSvc := agents.NewService(pool)
	settingsSvc := settings.NewService(pool)
	skillsSvc := appskills.NewService(pool)

	if err := seedSystemDataKey(ctx, pwSvc); err != nil {
		return nil, err
	}

	if err := seedCredentials(ctx, pwSvc); err != nil {
		return nil, err
	}

	if err := seedJWTSecret(ctx, pwSvc); err != nil {
		return nil, err
	}

	if err := seedDefaultUser(ctx, sysSvc); err != nil {
		return nil, err
	}

	if err := seedDefaultWorkspace(ctx, sysSvc); err != nil {
		return nil, err
	}

	if err := seedDefaultAgent(ctx, sysSvc, agentsSvc, settingsSvc); err != nil {
		return nil, err
	}

	if err := seedDefaultSkills(ctx, sysSvc, skillsSvc, bucket); err != nil {
		return nil, err
	}

	// This Lambda only runs SQL against an existing database; it doesn't
	// create any infrastructure for CloudFormation to track.
	// Return a fixed PhysicalResourceID (`astroclaw-database-migrations`) so CloudFormation never sees a change
	// and never triggers a replacement or delete cycle.
	return &Result{PhysicalResourceID: "astroclaw-database-migrations"}, nil
}

// runMigrations reads SQL files from the migrations/ directory (bundled with
// this Lambda via CDK's bundling configuration), splits each file into individual statements
// to comply with DSQL's one-DDL-per-transaction constraint, and executes
// them one by one. Progress is tracked per-statement in the _migrations table
// so that partial failures can resume from where they left off.
//
// DSQL requires one DDL statement per transaction, so each statement runs
// independently outside a transaction.
func runMigrations(ctx context.Context, pool *pgxpool.Pool, dsqlMode bool) error {
	// Create the tracking table if it doesn't exist.
	// This is safe to run on every invocation.

	// Tracking granularity is per-statement, not per-file. A single migration
	// file may contain multiple DDL statements (e.g., CREATE TABLE + CREATE INDEX).
	// DSQL can't run multiple DDL in one transaction, so if statement 2 fails,
	// statement 1 is already committed. statement_index lets us record exactly
	// which statements succeeded, so the next run resumes from the failure point
	// instead of re-running the whole file.
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS _migrations (
		filename TEXT,
		statement_index INT,
		applied_at TIMESTAMPTZ DEFAULT now(),
		PRIMARY KEY (filename, statement_index)
	)`)
	if err != nil {
		return fmt.Errorf("create tracking table: %w", err)
	}

	// Read migration files. These are bundled into the Lambda deployment
	// package by CDK alongside the bootstrap binary.
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(files)

	for _, f := range files {
		filename := filepath.Base(f)
		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", filename, err)
		}

		// Split the file into individual DDL statements by trailing semicolons.
		// Each statement runs in its own implicit transaction (DSQL requirement).
		stmts := splitStatements(string(content))

		for i, stmt := range stmts {
			// Check if this statement was already applied.
			var applied bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM _migrations WHERE filename = $1 AND statement_index = $2)",
				filename, i,
			).Scan(&applied)
			if err != nil {
				return fmt.Errorf("%s[%d]: check tracking table: %w", filename, i, err)
			}
			if applied {
				log.Printf("skip %s[%d] (already applied)", filename, i)
				continue
			}

			// DSQL requires CREATE INDEX ASYNC instead of CREATE INDEX.
			// Replace at runtime so migration files stay compatible with
			// standard PostgreSQL.
			// https://docs.aws.amazon.com/aurora-dsql/latest/userguide/working-with-create-index-async.html
			if dsqlMode {
				stmt = strings.Replace(stmt, "CREATE INDEX ", "CREATE INDEX ASYNC ", 1)
			}

			// Execute the statement. If it fails with "already exists"
			// (e.g., table, index, or column), treat it as already applied.
			// This handles the case where a previous run executed the DDL
			// successfully but failed to write the tracking record.
			// PostgreSQL error codes:
			//   42P07 = relation (table/index/view/sequence) already exists
			//   42701 = column already exists
			//   42710 = constraint already exists
			// https://www.postgresql.org/docs/16/errcodes-appendix.html
			if _, err := pool.Exec(ctx, stmt); err != nil {
				if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && (pgErr.Code == "42P07" || pgErr.Code == "42701" || pgErr.Code == "42710") {
					log.Printf("%s[%d]: already exists, treating as applied", filename, i)
				} else {
					return fmt.Errorf("%s[%d]: %w", filename, i, err)
				}
			}

			// Record it in the tracking table. DDL is auto-committed in DSQL
			// and can't be rolled back, so if this INSERT fails, we'd have an
			// applied DDL with no tracking record. Retry up to 3 times to
			// handle transient failures before giving up.
			var recordErr error
			for attempt := 1; attempt <= 3; attempt++ {
				_, recordErr = pool.Exec(ctx,
					"INSERT INTO _migrations (filename, statement_index) VALUES ($1, $2)",
					filename, i,
				)
				if recordErr == nil {
					break
				}
				log.Printf("%s[%d]: retry recording (attempt %d/3): %v", filename, i, attempt, recordErr)
				time.Sleep(time.Duration(attempt) * time.Second) // 1s, 2s, 3s
			}
			if recordErr != nil {
				return fmt.Errorf("%s[%d]: record migration after 3 attempts: %w", filename, i, recordErr)
			}

			log.Printf("applied %s[%d]", filename, i)
		}
	}

	return nil
}

// seedCredentials writes initial system-scope credentials into the database
// from environment variables set by CDK parameters. The system data key
// must already be provisioned (see seedSystemDataKey).
func seedCredentials(ctx context.Context, pwSvc *passwords.Service) error {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		if err := pwSvc.UpsertSystemCredential(ctx, passwords.SystemCredAnthropicAPIKey, "Anthropic API key, set via CDK parameter", key); err != nil {
			return fmt.Errorf("seed %s: %w", passwords.SystemCredAnthropicAPIKey, err)
		}
		log.Printf("seeded %s", passwords.SystemCredAnthropicAPIKey)
	}
	return nil
}

// seedSystemDataKey ensures the system-scope data key exists. Must run
// before any seedXxxCredential call that touches system credentials.
func seedSystemDataKey(ctx context.Context, pwSvc *passwords.Service) error {
	if err := pwSvc.ProvisionSystemDataKey(ctx); err != nil {
		return fmt.Errorf("provision system data key: %w", err)
	}
	return nil
}

// seedJWTSecret ensures a secret used to sign session JWTs exists in the
// system-scope credentials. The secret never appears in any env var or
// log output: it is generated on first deploy with crypto/rand, wrapped
// with the system data key, and reused on subsequent deploys.
func seedJWTSecret(ctx context.Context, pwSvc *passwords.Service) error {
	if _, err := pwSvc.GetSystemCredential(ctx, passwords.SystemCredJWTSecret); err == nil {
		log.Printf("%s already exists, skipping seed", passwords.SystemCredJWTSecret)
		return nil
	} else if !errors.Is(err, passwords.ErrNotFound) {
		return fmt.Errorf("check %s: %w", passwords.SystemCredJWTSecret, err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate %s: %w", passwords.SystemCredJWTSecret, err)
	}
	secret := base64.RawStdEncoding.EncodeToString(raw)

	if err := pwSvc.UpsertSystemCredential(ctx, passwords.SystemCredJWTSecret,
		"HMAC secret used to sign session JWTs (auto-generated, do not log)", secret); err != nil {
		return fmt.Errorf("store %s: %w", passwords.SystemCredJWTSecret, err)
	}
	log.Printf("seeded %s", passwords.SystemCredJWTSecret)
	return nil
}

// seedDefaultUser creates the admin user on the first deployment if none
// exists, and (re)sets the admin password whenever a fresh value is provided
// via GENERATED_ADMIN_PASSWORD. CDK only populates that env var when the
// stack is deployed with GenerateAdminPassword=true; on other deploys it is
// empty and we leave any existing password untouched.
func seedDefaultUser(ctx context.Context, svc *system.Service) error {
	newPassword := os.Getenv("GENERATED_ADMIN_PASSWORD")

	admin, err := svc.GetAdmin(ctx)
	if err != nil {
		// TODO: The admin email here is just a place holder, need a better way to set it.
		admin, err = svc.CreateUser(ctx, "admin@astroclaw.local", "Admin", system.RoleAdmin)
		if err != nil {
			return fmt.Errorf("seed admin user: %w", err)
		}
		log.Printf("seeded admin user: %s (%s)", admin.Name, admin.ID)
	} else {
		log.Println("admin user already exists, skipping user seed")
	}

	if newPassword != "" {
		if err := svc.SetUserPassword(ctx, admin.ID, newPassword); err != nil {
			return fmt.Errorf("set admin password: %w", err)
		}
		log.Printf("set admin password (length %d) for %s", len(newPassword), admin.ID)
	}

	return nil
}

// seedDefaultWorkspace creates a default workspace and adds the admin to it as
// owner, unless the admin is already a member of some workspace.
func seedDefaultWorkspace(ctx context.Context, svc *system.Service) error {
	admin, err := svc.GetAdmin(ctx)
	if err != nil {
		return fmt.Errorf("get admin for workspace seed: %w", err)
	}

	existing, err := svc.ListWorkspacesForUser(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("list workspaces for admin: %w", err)
	}
	if len(existing) > 0 {
		log.Println("admin already belongs to a workspace, skipping seed")
		return nil
	}

	w, err := svc.CreateWorkspace(ctx, "Default")
	if err != nil {
		return fmt.Errorf("seed default workspace: %w", err)
	}
	log.Printf("seeded default workspace: %s (%s)", w.Name, w.ID)

	if err := svc.AddMembership(ctx, admin.ID, w.ID, system.WorkspaceRoleOwner); err != nil {
		return fmt.Errorf("add admin to default workspace: %w", err)
	}
	log.Printf("added admin to default workspace as owner")

	return nil
}

// seedDefaultAgent creates the genesis agent in the default workspace on first
// deploy if none exists. This is the first agent created in the system, and is
// set as the default agent.
func seedDefaultAgent(ctx context.Context, sysSvc *system.Service, svc *agents.Service, settingsSvc *settings.Service) error {
	// Find the default workspace (the admin's first workspace).
	// Admin -> workspaces[0] -> workspaces[0].agents[]
	admin, err := sysSvc.GetAdmin(ctx)
	if err != nil {
		return fmt.Errorf("get admin for agent seed: %w", err)
	}
	workspaces, err := sysSvc.ListWorkspacesForUser(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("list admin workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("admin has no workspace to seed agent into")
	}
	workspaceID := workspaces[0].ID

	existing, err := svc.ListAgentsByWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	if len(existing) > 0 {
		log.Printf("agents already exist in workspace %s, skipping seed", workspaceID)
		return nil
	}

	defaultSoul := "You are AstroClaw, a personal AI assistant. " +
		"Be genuinely helpful, not performatively helpful. Skip filler words like 'Great question!' and just help. " +
		"Have opinions. Be direct. Admit uncertainty when appropriate. " +
		"Be resourceful before asking. Try to figure it out, then ask if stuck. " +
		"Be concise when needed, thorough when it matters."

	// TODO: model should come from CDK parameter or environment variable or some other way instead of hardcoded.
	a, err := svc.CreateAgent(ctx, workspaceID, "genesis", defaultSoul, "claude-haiku-4-5-20251001")
	if err != nil {
		return fmt.Errorf("seed genesis agent: %w", err)
	}
	log.Printf("seeded genesis agent: %s (%s) in workspace %s", a.Name, a.ID, workspaceID)

	// Set the genesis agent as the default agent.
	if err := settingsSvc.UpsertWorkspaceSetting(ctx, workspaceID, settings.SettingWorkspaceDefaultAgentID, a.ID); err != nil {
		return fmt.Errorf("seed default_agent_id setting: %w", err)
	}
	log.Printf("seeded default_agent_id: %s", a.ID)

	return nil
}

// splitStatements splits SQL content by semicolons at the end of a line.
// Skips empty statements and comment-only lines.
func splitStatements(content string) []string {
	var stmts []string

	// Note: this is a naive splitter that doesn't handle semicolons inside
	// strings or comments. The only realistic scenario would be a DEFAULT
	// value containing a semicolon (e.g., DEFAULT 'a;b'), which is uncommon
	// in DDL. If it ever occurs, the integration test should catch it.
	for _, raw := range strings.Split(content, ";") {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
		// Skip comment-only blocks (e.g. "-- atlas:txmode none").
		lines := strings.Split(stmt, "\n")
		hasSQL := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "--") {
				hasSQL = true
				break
			}
		}
		if hasSQL {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}

func main() {
	lambda.Start(handler)
}
