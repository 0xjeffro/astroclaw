// WebSocket $disconnect handler.
//
// API Gateway WebSocket invokes this Lambda when a client disconnects
// (either gracefully or due to timeout/network failure). It cleans up
// the connection record from app_system_connections so that Reply Lambda
// no longer tries to push events to a dead connection.
//
// IMPORTANT: $disconnect is best-effort. API Gateway will try its best to
// deliver this event, but cannot guarantee delivery. If this Lambda is not
// triggered, dead connections will remain in the database. The Reply Lambda
// handles this by detecting 410 Gone errors from PostToConnection and
// cleaning up stale records as a fallback.
//
// API Gateway WebSocket $disconnect docs:
// https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-route-keys-connect-disconnect.html
package main

import (
	"context"
	"log"
	"os"

	"astroclaw/pkg/app/system"
	"astroclaw/pkg/cloud/wsbus"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

var registry wsbus.Registry

func init() {
	ctx := context.Background()

	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		log.Fatalf("connect to DSQL: %v", err)
	}

	registry = wsbus.NewWsRegistry(system.NewService(pool))
}

func handler(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connectionID := req.RequestContext.ConnectionID

	if err := registry.Unregister(ctx, connectionID); err != nil {
		log.Printf("failed to delete connection %s: %v", connectionID, err)
	}

	log.Printf("disconnected: %s", connectionID)
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handler)
}
