package main

import (
	"context"
	"encoding/json"
	"iclaw/pkg/agent"
	"iclaw/pkg/app/chat"
	"iclaw/pkg/provider"
	"iclaw/pkg/tool"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aurora-dsql-connectors/go/pgx/dsql"
)

var svc *chat.Service

// init runs once when Lambda cold-starts. Connects to DSQL via IAM
// authentication, sets up LLM provider and chat service.
func init() {
	ctx := context.Background()

	// Connect to DSQL using IAM authentication.
	// DSQL_ENDPOINT is set by CDK from the cluster's endpoint attribute.
	// TODO: support standard PostgreSQL via pgxpool.New() for users who choose Aurora PostgreSQL over DSQL in CDK config.
	// Go connector docs: https://docs.aws.amazon.com/aurora-dsql/latest/userguide/SECTION_program-with-go-pgx-connector.html
	pool, err := dsql.NewPool(ctx, dsql.Config{
		Host: os.Getenv("DSQL_ENDPOINT"),
	})
	if err != nil {
		log.Fatalf("connect to DSQL: %v", err)
	}

	// LLM Provider
	// TODO: API key management is TBD. Hardcoded placeholder for now.
	var p provider.Provider
	model := os.Getenv("MODEL_NAME")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	p = provider.NewAnthropic("sk-ant-placeholder", model)

	registry := tool.NewRegistry()
	registry.Register(tool.GetCurrentTime)
	registry.Register(tool.EvalArithmetic)
	registry.Register(tool.ReadFile)
	// TODO: remove ExecCommand tool. In Lambda, a prompt-injected Agent could
	// use ExecCommand to access DSQL credentials and compromise the database.
	registry.Register(tool.ExecCommand)
	registry.Register(tool.WriteFile)
	registry.Register(tool.EditFile)

	systemPrompt := "You are iClaw, an intelligent assistant that can perform various tasks by calling tools. " +
		"Use tools to get information, manipulate files, execute commands, and more. " +
		"Always think step by step and use tools whenever appropriate. " +
		"Only respond with plain text, do not use LaTeX or markdown formatting."

	createFn := func(s *chat.Session) *agent.Agent {
		return agent.NewFromContext(
			p, systemPrompt, registry, 128000,
			chat.ToProviderMessages(s.ContextMessages), s.ContextSummary,
		)
	}

	svc = chat.NewService(pool, createFn, nil)
}

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	path := req.RequestContext.HTTP.Path
	method := req.RequestContext.HTTP.Method

	switch {
	case method == "POST" && path == "/sessions":
		return handleCreateSession(ctx, req)
	case method == "GET" && path == "/sessions":
		return handleListSessions(ctx)
	case method == "GET" && strings.HasPrefix(path, "/sessions/") && !strings.Contains(path, "/reply"):
		id := strings.TrimPrefix(path, "/sessions/")
		return handleGetSession(ctx, id)
	case method == "DELETE" && strings.HasPrefix(path, "/sessions/"):
		id := strings.TrimPrefix(path, "/sessions/")
		return handleDeleteSession(ctx, id)
	case method == "POST" && strings.HasSuffix(path, "/reply"):
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 3 {
			return handleReply(ctx, parts[1], req)
		}
	}

	return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"})
}

func handleCreateSession(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	var body struct {
		UserID string `json:"user_id"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}

	s, err := svc.NewSession(ctx, body.UserID, body.Title)
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

func handleReply(ctx context.Context, sessionID string, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}

	reply, err := svc.Reply(ctx, sessionID, body.Text)
	if err != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return jsonResponse(http.StatusOK, map[string]string{"reply": reply})
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
