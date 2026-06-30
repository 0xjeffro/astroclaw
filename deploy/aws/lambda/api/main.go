package main

import (
	"astroclaw/pkg/api"
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"
	"astroclaw/pkg/auth"
	"astroclaw/pkg/crypto"
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
)

// buildLambdaHandler connects to DSQL, wires the services, and returns a
// Lambda-compatible handler. Pulled out of init() so `go test` can import
// this package without forcing a real DSQL connection.
func buildLambdaHandler() *httpadapter.HandlerAdapterV2 {
	ctx := context.Background()

	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		log.Fatalf("connect to DSQL: %v", err)
	}

	km, err := crypto.OpenKeyManager(ctx, os.Getenv("KMS_URL"))
	if err != nil {
		log.Fatalf("open key manager: %v", err)
	}
	pwSvc := passwords.NewService(pool, km)
	secretLoader := auth.NewSecretLoader(pwSvc)

	chatSvc := chat.NewService(pool, nil)
	settingsSvc := settings.NewService(pool)
	systemSvc := system.NewService(pool)

	return httpadapter.NewV2(api.NewRouter(api.RouterConfig{
		Chat:      chatSvc,
		Settings:  settingsSvc,
		System:    systemSvc,
		GetSecret: secretLoader.Get,
	}))
}

func main() {
	lambda.Start(buildLambdaHandler().ProxyWithContext)
}
