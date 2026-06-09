package main

import (
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"
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

	chatSvc := chat.NewService(pool, nil)
	settingsSvc := settings.NewService(pool)
	systemSvc := system.NewService(pool)

	return httpadapter.NewV2(newRouter(chatSvc, settingsSvc, systemSvc))
}

func main() {
	lambda.Start(buildLambdaHandler().ProxyWithContext)
}
