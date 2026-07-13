package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations reads *.sql files from migrationsDir, splits each into
// individual statements, and applies them one by one, tracking progress
// in a _migrations table so partial failures can resume.
//
// Migration files target vanilla PostgreSQL. When dsqlMode is true two
// DSQL-specific adjustments kick in:
//   - CREATE INDEX is rewritten to CREATE INDEX ASYNC
//   - Each statement runs outside a transaction (DSQL's one-DDL-per-txn rule)
//
// On Postgres both behaviours are still correct, they are just
// unnecessarily conservative — we accept that to keep one codepath.
//
// https://docs.aws.amazon.com/aurora-dsql/latest/userguide/working-with-create-index-async.html
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, dsqlMode bool, migrationsDir string) error {
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}

	// Tracking granularity is per-statement, not per-file. A single file
	// may contain multiple DDL statements; if statement 2 fails and 1 is
	// already committed, statement_index lets us resume from the failure.
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS _migrations (
		filename TEXT,
		statement_index INT,
		applied_at TIMESTAMPTZ DEFAULT now(),
		PRIMARY KEY (filename, statement_index)
	)`)
	if err != nil {
		return fmt.Errorf("create tracking table: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
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

		stmts := splitStatements(string(content))

		for i, stmt := range stmts {
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

			if dsqlMode {
				stmt = strings.Replace(stmt, "CREATE INDEX ", "CREATE INDEX ASYNC ", 1)
			}

			// PostgreSQL error codes for idempotent "already exists":
			//   42P07 = relation (table/index/view/sequence)
			//   42701 = column
			//   42710 = constraint
			// https://www.postgresql.org/docs/16/errcodes-appendix.html
			if _, err := pool.Exec(ctx, stmt); err != nil {
				if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && (pgErr.Code == "42P07" || pgErr.Code == "42701" || pgErr.Code == "42710") {
					log.Printf("%s[%d]: already exists, treating as applied", filename, i)
				} else {
					return fmt.Errorf("%s[%d]: %w", filename, i, err)
				}
			}

			// DDL is auto-committed and can't be rolled back on DSQL, so if
			// this INSERT fails we would have an applied DDL with no tracking
			// row. Retry up to 3 times before giving up.
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
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			if recordErr != nil {
				return fmt.Errorf("%s[%d]: record migration after 3 attempts: %w", filename, i, recordErr)
			}

			log.Printf("applied %s[%d]", filename, i)
		}
	}

	return nil
}

// splitStatements splits a SQL file at end-of-line semicolons, skipping
// empty and comment-only chunks. It's a naive splitter — semicolons
// inside string literals or comments are treated as separators.
// Acceptable for our DDL, which never uses embedded semicolons.
func splitStatements(content string) []string {
	var stmts []string
	for _, raw := range strings.Split(content, ";") {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
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
