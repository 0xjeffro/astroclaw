package main

import (
	"astroclaw/pkg/app/chat"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

var (
	svc         *chat.Service
	settingsSvc *settings.Service
	systemSvc   *system.Service
	apiKey      string
)

// init runs once when Lambda cold-starts. Connects to DSQL and sets up
// the chat service for session CRUD operations only. (No LLM provider
// or tools needed here cuz reply is handled by the Reply Lambda.)
func init() {
	ctx := context.Background()

	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		log.Fatalf("connect to DSQL: %v", err)
	}

	// Read API authentication key.
	pwSvc := passwords.NewService(pool)
	apiKeyCred, err := pwSvc.GetCredentialByName(ctx, "api-key")
	if err != nil {
		log.Println("warning: no api-key in credentials table, all requests will be allowed without authentication")
	} else {
		apiKey = apiKeyCred.Value
	}

	settingsSvc = settings.NewService(pool)
	systemSvc = system.NewService(pool)

	// Session CRUD doesn't need LLM provider or tools, pass nil for createFn.
	svc = chat.NewService(pool, nil)
}

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if apiKey != "" && req.Headers["x-api-key"] != apiKey {
		return jsonResponse(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	path := req.RequestContext.HTTP.Path
	method := req.RequestContext.HTTP.Method

	switch {
	case method == "POST" && path == "/sessions":
		return handleCreateSession(ctx, req)
	case method == "GET" && path == "/sessions":
		return handleListSessions(ctx)
	case method == "GET" && strings.HasPrefix(path, "/sessions/"):
		id := strings.TrimPrefix(path, "/sessions/")
		return handleGetSession(ctx, id)
	case method == "DELETE" && strings.HasPrefix(path, "/sessions/"):
		id := strings.TrimPrefix(path, "/sessions/")
		return handleDeleteSession(ctx, id)
	case method == "GET" && strings.HasPrefix(path, "/settings/"):
		name := strings.TrimPrefix(path, "/settings/")
		return handleGetSetting(ctx, name)
	case method == "GET" && path == "/users/owner":
		return handleGetOwner(ctx)
	}

	return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"})
}

func handleCreateSession(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	var body struct {
		UserID   string   `json:"user_id"`
		AgentIDs []string `json:"agent_ids"`
		Title    string   `json:"title"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}

	s, err := svc.NewSession(ctx, body.UserID, body.AgentIDs, body.Title)
	if err != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusCreated, s)
}

func handleListSessions(ctx context.Context) (events.APIGatewayV2HTTPResponse, error) {
	sessions, err := svc.ListSessions(ctx)
	if err != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusOK, sessions)
}

func handleGetSession(ctx context.Context, id string) (events.APIGatewayV2HTTPResponse, error) {
	s, err := svc.GetSession(ctx, id)
	if err != nil {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusOK, s)
}

func handleDeleteSession(ctx context.Context, id string) (events.APIGatewayV2HTTPResponse, error) {
	if err := svc.SoftDeleteSession(ctx, id); err != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusOK, map[string]string{"status": "deleted"})
}

func handleGetSetting(ctx context.Context, name string) (events.APIGatewayV2HTTPResponse, error) {
	s, err := settingsSvc.GetKVSetting(ctx, name)
	if err != nil {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusOK, s)
}

func handleGetOwner(ctx context.Context) (events.APIGatewayV2HTTPResponse, error) {
	u, err := systemSvc.GetOwner(ctx)
	if err != nil {
		return jsonResponse(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusOK, u)
}

func jsonResponse(status int, body any) (events.APIGatewayV2HTTPResponse, error) {
	b, _ := json.Marshal(body)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}, nil
}

func main() {
	lambda.Start(handler)
}
