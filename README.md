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

## Database

CDK deployment currently defaults to Aurora DSQL. A future release will add a configuration option to choose between DSQL and Aurora PostgreSQL.

- **DSQL**: Lower cost (true serverless pay-per-request, scale to zero). Best for most workloads.
- **Aurora PostgreSQL**: Full PostgreSQL feature set (JSONB columns, foreign keys, extensions). Choose this if you need vector search (pgvector) or other advanced query capabilities.

## Development

### AWS Tools
- [Installing or updating to the latest version of the AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- [Getting started with the AWS CDK](https://docs.aws.amazon.com/cdk/v2/guide/getting-started.html)

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
