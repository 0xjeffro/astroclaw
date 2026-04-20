# iClaw

AWS-native AI agent with one-click CDK deployment.

iClaw is a cloud-native personal AI agent that can chat, call tools, manage sessions, and interact with custom Apps (calendar, tasks, etc.). It supports multiple LLM providers (OpenAI, Anthropic) and is designed for serverless deployment on AWS.

## Quick Start

[Getting Started](docs/getting-started.md)

## Development

[Developer Guide](docs/development.md)

## Deployment

[Deployment Guide](docs/deployment.md)


```
atlas migrate diff create_tables --env local
sqlc generate
```
