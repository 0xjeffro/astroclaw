// astroclaw-migrate is a one-shot binary that applies pending schema
// migrations and idempotent seed data to a Postgres database, then
// exits.
//
// docker-compose runs it as an init container that gates the main
// server: astroclaw depends_on migrate with condition:
// service_completed_successfully.
//
// Env:
//
//	DATABASE_URL          required     postgres://user:pass@host/db
//	KMS_URL               required     base64key://<b64-master-key> or awskms://...
//	STORAGE_URL           required     file:///data/skills / s3://... / mem://
//	ANTHROPIC_API_KEY     optional     seeded as system credential when set
//	ADMIN_PASSWORD        optional     sets or resets the admin password
//	MIGRATIONS_DIR        optional     default "/migrations"
//	SKILLS_DIR            optional     default "/skills"
package main

import (
	"context"
	"log"
	"os"

	"astroclaw/pkg/bootstrap"
	"astroclaw/pkg/cloud"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()

	km, err := cloud.OpenKeyManager(ctx, mustEnv("KMS_URL"))
	if err != nil {
		log.Fatalf("open key manager: %v", err)
	}
	bucket, err := cloud.OpenBucket(ctx, mustEnv("STORAGE_URL"))
	if err != nil {
		log.Fatalf("open storage bucket: %v", err)
	}
	defer func() { _ = bucket.Close() }()

	if err := bootstrap.Run(ctx, bootstrap.Config{
		Pool:            pool,
		KeyManager:      km,
		Bucket:          bucket,
		DSQLMode:        false,
		MigrationsDir:   envOr("MIGRATIONS_DIR", "/migrations"),
		SkillsDir:       envOr("SKILLS_DIR", "/skills"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		AdminPassword:   os.Getenv("ADMIN_PASSWORD"),
	}); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	log.Println("astroclaw-migrate: done")
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env %s is not set", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
