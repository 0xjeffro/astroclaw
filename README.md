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

## App Permission Model

Apps are divided into two categories with different security boundaries:

**System Apps** (e.g. Chat, Calendar, Task)
- Maintained by the project maintainers.
- Share a single Lambda and database connection.
- Access control is enforced at the code level: each App only imports its own `db.Queries` package.
- Use `DbConnectAdmin` (full database access) since the code is trusted.

**Third-party Apps** (future)
- Developed by external contributors.
- Each third-party App runs in its own Lambda with a dedicated database Role, restricted to only the tables it creates/owns.
- IAM permission: `dsql:DbConnect` (not Admin).
- Database role mapping via `GRANT CONNECT TO '<lambda-role-arn>' WITH <app_role>`.
- More details are yet to be planned and discussed.

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
