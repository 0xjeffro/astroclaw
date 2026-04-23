package main

import (
	"testing"
)

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
