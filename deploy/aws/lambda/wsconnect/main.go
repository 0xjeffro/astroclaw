// WebSocket $connect handler.
//
// API Gateway WebSocket requires this Lambda for the $connect route. This Lambda
// is invoked every time a client establishes a WebSocket connection. It is
// responsible for:
//  1. Authenticating the client (currently via api_key in query string).
//  2. Storing the connectionId and userId in app_system_connections so that
//     other Lambdas (e.g. Reply) can push events to this connection.
//
// The client connects with:
//
//	wss://xxx.execute-api.us-east-1.amazonaws.com?user_id=xxx&api_key=xxx
//
// TODO: this is a single-user system for now, using a api_key for
// authentication. When multi-user support is added, may be we should replace api_key with
// JWT tokens so the server can verify user identity without trusting the
// client-provided user_id.
//
// API Gateway WebSocket $connect docs:
// https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-route-keys-connect-disconnect.html
package main

import (
	"context"
	"log"
	"os"

	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/system"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

var (
	systemSvc *system.Service
	apiKey    string
)

func init() {
	ctx := context.Background()

	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		log.Fatalf("connect to DSQL: %v", err)
	}

	pwSvc := passwords.NewService(pool)
	apiKeyCred, err := pwSvc.GetCredentialByName(ctx, "api-key")
	if err != nil {
		log.Println("warning: no api-key in credentials table, all connections will be allowed")
	} else {
		apiKey = apiKeyCred.Value
	}

	systemSvc = system.NewService(pool)
}

func handler(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Authenticate.
	if apiKey != "" {
		clientKey := req.QueryStringParameters["api_key"]
		if clientKey != apiKey {
			return events.APIGatewayProxyResponse{StatusCode: 403}, nil
		}
	}

	// Extract user_id and workspace_id from query string.
	// TODO: replace query params with JWT-based auth so the server can verify
	// identity without trusting client-provided IDs.
	userID := req.QueryStringParameters["user_id"]
	workspaceID := req.QueryStringParameters["workspace_id"]
	if userID == "" || workspaceID == "" {
		return events.APIGatewayProxyResponse{StatusCode: 400}, nil
	}

	// Verify the user is a member of this workspace.
	if _, err := systemSvc.GetMembership(ctx, userID, workspaceID); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 403}, nil
	}

	// Store connection.
	connectionID := req.RequestContext.ConnectionID
	if err := systemSvc.CreateWSConnectRecord(ctx, connectionID, userID, workspaceID); err != nil {
		log.Printf("failed to store connection %s: %v", connectionID, err)
		return events.APIGatewayProxyResponse{StatusCode: 500}, nil
	}

	log.Printf("connected: %s (user: %s, workspace: %s)", connectionID, userID, workspaceID)
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handler)
}
