// TODO: add a CI workflow (.github/workflows/atlas-check.yml) that runs
// [Done] `atlas migrate diff --env local` on every PR and `git diff --exit-code`
// to ensure schema changes have a corresponding migration file committed.
// Register the job as a required status check in branch protection so a PR
// can't merge with schema changes but no matching migration.
//
// [Done] Also add a registry check: a script that scans pkg/app/*/db/schema.sql and
// verifies every file appears in the src list below. Otherwise a brand-new
// app is silently skipped (atlas migrate diff sees no change, git diff passes).
//
// For final enforcement: a workflow that requires a "schema-reviewed" label
// on PRs touching schema.sql. Reviewer adds the label only after confirming
// atlas.hcl / sqlc.yaml were updated correctly. Combined with branch
// protection, this blocks merge until human review of the registry change.
env "local" {
  src = [
    "file://pkg/app/chat/db/schema.sql",
    "file://pkg/app/skills/db/schema.sql",
    "file://pkg/app/passwords/db/schema.sql",
    "file://pkg/app/settings/db/schema.sql",
    "file://pkg/app/notes/db/schema.sql",
    "file://pkg/app/agents/db/schema.sql",
    "file://pkg/app/system/db/schema.sql",
  ]
  dev = "docker://postgres/16/dev"
  migration {
    dir = "file://migrations"
  }
}
