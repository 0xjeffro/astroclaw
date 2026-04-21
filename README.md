# iClaw

AWS-native AI agent with one-click CDK deployment.

## Prerequisites

- Go 1.22+
- Docker (for local PostgreSQL)
- OpenAI or Anthropic API key

## Run Locally

```bash
# With Anthropic
ANTHROPIC_API_KEY=sk-ant-xxx go run .

# Or with OpenAI
OPENAI_API_KEY=sk-xxx go run .
```

A temporary PostgreSQL container starts automatically. Data is lost when the process exits.

To persist data across restarts, use an existing database:

```bash
DATABASE_URL="postgres://user:pass@localhost:5432/iclaw" \
ANTHROPIC_API_KEY=sk-ant-xxx go run .
```

## Development

### Generate code after schema/query changes

```bash
atlas migrate diff <migration_name> --env local
sqlc generate
```

### Run tests

```bash
go test ./...
```

Tests auto-start a PostgreSQL container via testcontainers. No manual setup needed.
