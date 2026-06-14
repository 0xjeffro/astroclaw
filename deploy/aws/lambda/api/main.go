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
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
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

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}
	km := crypto.NewAWSKMSKeyManager(kms.NewFromConfig(awsCfg), os.Getenv("PASSWORDS_KMS_KEY_ID"))
	pwSvc := passwords.NewService(pool, km)
	secretLoader := auth.NewSecretLoader(pwSvc)

	chatSvc := chat.NewService(pool, nil)
	settingsSvc := settings.NewService(pool)
	systemSvc := system.NewService(pool)

	return httpadapter.NewV2(api.NewRouter(chatSvc, settingsSvc, systemSvc, secretLoader.Get))
}

func main() {
	lambda.Start(buildLambdaHandler().ProxyWithContext)
}
