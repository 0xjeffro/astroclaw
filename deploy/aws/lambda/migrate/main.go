// Package main is the AWS migrate Lambda entrypoint.
//
// It implements the CloudFormation Custom Resource protocol (Create /
// Update / Delete events) and delegates the actual work — schema
// migration + baseline seeding — to pkg/bootstrap so the same logic
// runs on docker deploys too.
//
// DSQL specifics (per-statement execution, CREATE INDEX ASYNC rewrite)
// are toggled via bootstrap.Config.DSQLMode, not duplicated here.
package main

import (
	"context"
	"fmt"
	"os"

	"astroclaw/pkg/bootstrap"
	"astroclaw/pkg/cloud"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

// Event and Result follow the CloudFormation Custom Resource protocol.
// https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/crpg-ref-requests.html
// https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.custom_resources.Provider.html
type Event struct {
	RequestType        string `json:"RequestType"`        // "Create" | "Update" | "Delete"
	PhysicalResourceID string `json:"PhysicalResourceId"` // CloudFormation's identifier for this resource
}

type Result struct {
	PhysicalResourceID string `json:"PhysicalResourceId"`
}

func handler(ctx context.Context, event Event) (*Result, error) {
	if event.RequestType == "Delete" {
		return &Result{PhysicalResourceID: event.PhysicalResourceID}, nil
	}

	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to DSQL: %w", err)
	}
	defer pool.Close()

	km, err := cloud.OpenKeyManager(ctx, os.Getenv("KMS_URL"))
	if err != nil {
		return nil, fmt.Errorf("open key manager: %w", err)
	}
	bucket, err := cloud.OpenBucket(ctx, os.Getenv("STORAGE_URL"))
	if err != nil {
		return nil, fmt.Errorf("open storage bucket: %w", err)
	}
	defer bucket.Close()

	if err := bootstrap.Run(ctx, bootstrap.Config{
		Pool:            pool,
		KeyManager:      km,
		Bucket:          bucket,
		DSQLMode:        true,
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		AdminPassword:   os.Getenv("GENERATED_ADMIN_PASSWORD"),
	}); err != nil {
		return nil, err
	}

	// This Lambda only writes to an existing database, no CFN-tracked
	// resources are created. Return a fixed PhysicalResourceID so CFN
	// never sees a change and never triggers replacement/delete cycles.
	return &Result{PhysicalResourceID: "astroclaw-database-migrations"}, nil
}

func main() {
	lambda.Start(handler)
}
