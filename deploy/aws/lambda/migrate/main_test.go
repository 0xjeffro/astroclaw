package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testPools holds two database connections: one for direct SQL execution
// (the "truth"), one for runMigrations() (the code under test).
var (
	directPool   *pgxpool.Pool // Database A: migration SQL executed directly
	migratedPool *pgxpool.Pool // Database B: migration SQL executed via runMigrations()
	migrationDir string        // absolute path to the project's migrations/ directory
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Locate the migrations directory (4 levels up from this test file).
	projectRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		panic("resolve project root: " + err.Error())
	}
	migrationDir = filepath.Join(projectRoot, "migrations")

	// Start a PostgreSQL container with two databases.
	pg, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("iclaw_direct"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
		postgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		panic("start PostgreSQL container: " + err.Error())
	}

	// Connect to the first database (direct execution).
	connDirect, _ := pg.ConnectionString(ctx, "sslmode=disable")
	directPool, err = pgxpool.New(ctx, connDirect)
	if err != nil {
		panic("connect to direct database: " + err.Error())
	}

	// Create the second database and connect to it.
	if _, err := directPool.Exec(ctx, "CREATE DATABASE iclaw_migrated"); err != nil {
		panic("create second database: " + err.Error())
	}
	migratedConfig, err := pgxpool.ParseConfig(connDirect)
	if err != nil {
		panic("parse connection config: " + err.Error())
	}
	migratedConfig.ConnConfig.Database = "iclaw_migrated"
	migratedPool, err = pgxpool.NewWithConfig(ctx, migratedConfig)
	if err != nil {
		panic("connect to migrated database: " + err.Error())
	}

	// Database A: execute migration SQL files directly, no runMigrations().
	files, _ := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	sort.Strings(files)
	for _, f := range files {
		content, _ := os.ReadFile(f)
		if _, err := directPool.Exec(ctx, string(content)); err != nil {
			panic(fmt.Sprintf("direct exec %s: %v", filepath.Base(f), err))
		}
	}

	// Database B: execute via runMigrations() (the code under test).
	// runMigrations() uses filepath.Glob("migrations/*.sql"), so we
	// chdir to the project root where the migrations/ directory lives.
	origDir, err := os.Getwd()
	if err != nil {
		panic("get working directory: " + err.Error())
	}
	if err := os.Chdir(projectRoot); err != nil {
		panic("chdir to project root: " + err.Error())
	}

	if err := runMigrations(ctx, migratedPool); err != nil {
		panic("runMigrations: " + err.Error())
	}

	if err := os.Chdir(origDir); err != nil {
		panic("chdir back: " + err.Error())
	}

	code := m.Run()

	directPool.Close()
	migratedPool.Close()
	if pg != nil {
		_ = pg.Terminate(ctx)
	}
	os.Exit(code)
}

// columnInfo represents a column's name, type, nullability, and default value.
type columnInfo struct {
	Table    string
	Column   string
	Type     string
	Nullable string // "YES" or "NO"
	Default  string // e.g. "gen_random_uuid()", "now()", ""
}

// indexInfo represents an index on a table.
type indexInfo struct {
	Table string
	Index string
}

// constraintInfo represents a table constraint (PRIMARY KEY, UNIQUE, CHECK).
// Constraint name is excluded because PostgreSQL generates internal OIDs
// that differ between databases (e.g. "2200_16386_1_not_null" vs
// "2200_16444_1_not_null"), even though the constraints are identical.
type constraintInfo struct {
	Table string
	Type  string // "PRIMARY KEY", "UNIQUE", "CHECK"
}

// queryColumns returns all user-defined columns from information_schema,
// sorted by table name and column name for deterministic comparison.
func queryColumns(ctx context.Context, pool *pgxpool.Pool) ([]columnInfo, error) {
	rows, err := pool.Query(ctx, `
		SELECT table_name, column_name, data_type,
		       is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, column_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []columnInfo
	for rows.Next() {
		var c columnInfo
		if err := rows.Scan(&c.Table, &c.Column, &c.Type, &c.Nullable, &c.Default); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// queryIndexes returns all user-defined indexes from pg_indexes.
func queryIndexes(ctx context.Context, pool *pgxpool.Pool) ([]indexInfo, error) {
	rows, err := pool.Query(ctx, `
		SELECT tablename, indexname
		FROM pg_indexes
		WHERE schemaname = 'public'
		ORDER BY tablename, indexname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var idxs []indexInfo
	for rows.Next() {
		var idx indexInfo
		if err := rows.Scan(&idx.Table, &idx.Index); err != nil {
			return nil, err
		}
		idxs = append(idxs, idx)
	}
	return idxs, rows.Err()
}

// queryConstraints returns all table constraints from information_schema.
func queryConstraints(ctx context.Context, pool *pgxpool.Pool) ([]constraintInfo, error) {
	rows, err := pool.Query(ctx, `
		SELECT table_name, constraint_type
		FROM information_schema.table_constraints
		WHERE table_schema = 'public'
		ORDER BY table_name, constraint_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cons []constraintInfo
	for rows.Next() {
		var c constraintInfo
		if err := rows.Scan(&c.Table, &c.Type); err != nil {
			return nil, err
		}
		cons = append(cons, c)
	}
	return cons, rows.Err()
}

// filterTable removes entries belonging to a specific table (used to
// exclude the _migrations tracking table from Database B).
func filterColumns(cols []columnInfo, excludeTable string) []columnInfo {
	var out []columnInfo
	for _, c := range cols {
		if c.Table != excludeTable {
			out = append(out, c)
		}
	}
	return out
}

func filterIndexes(idxs []indexInfo, excludeTable string) []indexInfo {
	var out []indexInfo
	for _, idx := range idxs {
		if idx.Table != excludeTable {
			out = append(out, idx)
		}
	}
	return out
}

func filterConstraints(cons []constraintInfo, excludeTable string) []constraintInfo {
	var out []constraintInfo
	for _, c := range cons {
		if c.Table != excludeTable {
			out = append(out, c)
		}
	}
	return out
}

// Verifies that runMigrations() produces the same schema as executing
// migration SQL files directly. Compares columns (name, type, nullable,
// default), indexes, and constraints between the two databases.
func TestRunMigrations_SchemaMatchesDirect(t *testing.T) {
	ctx := context.Background()

	// Compare columns.
	t.Run("columns", func(t *testing.T) {
		direct, err := queryColumns(ctx, directPool)
		if err != nil {
			t.Fatalf("query direct: %v", err)
		}
		migrated, err := queryColumns(ctx, migratedPool)
		if err != nil {
			t.Fatalf("query migrated: %v", err)
		}
		migrated = filterColumns(migrated, "_migrations")

		if len(direct) != len(migrated) {
			t.Fatalf("count mismatch: direct=%d, migrated=%d", len(direct), len(migrated))
		}
		for i := range direct {
			if direct[i] != migrated[i] {
				t.Errorf("column[%d] mismatch:\n  direct:   %+v\n  migrated: %+v", i, direct[i], migrated[i])
			}
		}
	})

	// Compare indexes.
	t.Run("indexes", func(t *testing.T) {
		direct, err := queryIndexes(ctx, directPool)
		if err != nil {
			t.Fatalf("query direct: %v", err)
		}
		migrated, err := queryIndexes(ctx, migratedPool)
		if err != nil {
			t.Fatalf("query migrated: %v", err)
		}
		migrated = filterIndexes(migrated, "_migrations")

		if len(direct) != len(migrated) {
			t.Fatalf("count mismatch: direct=%d, migrated=%d", len(direct), len(migrated))
		}
		for i := range direct {
			if direct[i] != migrated[i] {
				t.Errorf("index[%d] mismatch:\n  direct:   %+v\n  migrated: %+v", i, direct[i], migrated[i])
			}
		}
	})

	// Compare constraints (PRIMARY KEY, UNIQUE, CHECK).
	t.Run("constraints", func(t *testing.T) {
		direct, err := queryConstraints(ctx, directPool)
		if err != nil {
			t.Fatalf("query direct: %v", err)
		}
		migrated, err := queryConstraints(ctx, migratedPool)
		if err != nil {
			t.Fatalf("query migrated: %v", err)
		}
		migrated = filterConstraints(migrated, "_migrations")

		if len(direct) != len(migrated) {
			t.Fatalf("count mismatch: direct=%d, migrated=%d", len(direct), len(migrated))
		}
		for i := range direct {
			if direct[i] != migrated[i] {
				t.Errorf("constraint[%d] mismatch:\n  direct:   %+v\n  migrated: %+v", i, direct[i], migrated[i])
			}
		}
	})
}

// Double check: run runMigrations() again on an already-migrated database.
// Statements should be skipped via the tracking table, or if tracking fails,
// caught by the "already exists" error detection. Either way, it should complete without error.
func TestRunMigrations_SecondRun(t *testing.T) {
	ctx := context.Background()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir to project root: %v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Errorf("chdir back: %v", err)
		}
	}()

	// Second run should succeed with no errors.
	if err := runMigrations(ctx, migratedPool); err != nil {
		t.Fatalf("second runMigrations failed: %v", err)
	}
}

// Verifies that the _migrations tracking table has the correct number
// of records (one per statement in the migration files).
func TestRunMigrations_TrackingRecords(t *testing.T) {
	ctx := context.Background()

	// Count expected statements by splitting migration files.
	files, _ := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	sort.Strings(files)
	expectedCount := 0
	for _, f := range files {
		content, _ := os.ReadFile(f)
		expectedCount += len(splitStatements(string(content)))
	}

	var actualCount int
	err := migratedPool.QueryRow(ctx, "SELECT COUNT(*) FROM _migrations").Scan(&actualCount)
	if err != nil {
		t.Fatalf("query _migrations: %v", err)
	}

	if actualCount != expectedCount {
		t.Errorf("tracking records: got %d, want %d", actualCount, expectedCount)
	}
}

// --- Unit tests for splitStatements ---

// Verifies that multiple DDL statements separated by semicolons
// are correctly split into individual statements.
func TestSplitStatements_MultipleStatements(t *testing.T) {
	input := `CREATE TABLE users (id UUID PRIMARY KEY);
CREATE TABLE sessions (id UUID PRIMARY KEY);
CREATE INDEX idx ON sessions(id);`

	stmts := splitStatements(input)
	want := []string{
		"CREATE TABLE users (id UUID PRIMARY KEY)",
		"CREATE TABLE sessions (id UUID PRIMARY KEY)",
		"CREATE INDEX idx ON sessions(id)",
	}
	assertStatements(t, stmts, want)
}

// Verifies that comment-only blocks (no actual SQL) are filtered out.
// Atlas may prepend directives like "-- atlas:txmode none" that should
// not be treated as executable statements.
func TestSplitStatements_CommentOnlyBlock(t *testing.T) {
	input := `-- atlas:txmode none
-- this is a comment;
CREATE TABLE users (id UUID PRIMARY KEY);`

	stmts := splitStatements(input)
	want := []string{
		"CREATE TABLE users (id UUID PRIMARY KEY)",
	}
	assertStatements(t, stmts, want)
}

// Verifies that comments preceding a SQL statement are preserved as
// part of the statement, not stripped or treated as a separate block.
func TestSplitStatements_CommentBeforeSQL(t *testing.T) {
	input := `-- Create users table
CREATE TABLE users (id UUID PRIMARY KEY);`

	stmts := splitStatements(input)
	want := []string{
		"-- Create users table\nCREATE TABLE users (id UUID PRIMARY KEY)",
	}
	assertStatements(t, stmts, want)
}

// Verifies that a trailing semicolon at the end of the file does not
// produce an extra empty statement.
func TestSplitStatements_TrailingSemicolon(t *testing.T) {
	input := `CREATE TABLE users (id UUID PRIMARY KEY);
`

	stmts := splitStatements(input)
	want := []string{
		"CREATE TABLE users (id UUID PRIMARY KEY)",
	}
	assertStatements(t, stmts, want)
}

// Verifies that an empty input returns an empty slice, not nil or panic.
func TestSplitStatements_EmptyInput(t *testing.T) {
	stmts := splitStatements("")
	if len(stmts) != 0 {
		t.Fatalf("got %d statements, want 0", len(stmts))
	}
}

// Verifies that a single statement without any extras is returned as-is.
func TestSplitStatements_SingleStatement(t *testing.T) {
	input := `CREATE TABLE users (id UUID PRIMARY KEY);`

	stmts := splitStatements(input)
	want := []string{
		"CREATE TABLE users (id UUID PRIMARY KEY)",
	}
	assertStatements(t, stmts, want)
}

func assertStatements(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement[%d]:\n  got:  %q\n  want: %q", i, got[i], want[i])
		}
	}
}
