// Package aws implements the wsbus interfaces for the AWS API Gateway
// WebSocket model: the gateway owns the socket, we push via the
// @connections HTTP side channel, and connection state lives in DSQL
// because Lambda is stateless.
package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types"

	"astroclaw/pkg/cloud/wsbus"
)

// Bus pushes WebSocket payloads via AWS API Gateway's @connections HTTP
// side channel. Lambda cannot hold a persistent socket itself, so all
// pushes go through the gateway.
//
// https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-how-to-call-websocket-api-connections.html
type Bus struct {
	client *apigatewaymanagementapi.Client
}

// NewBus wraps an already-configured apigatewaymanagementapi.Client.
// The caller is responsible for setting its BaseEndpoint to the WS
// management endpoint of the deployed stage.
func NewBus(client *apigatewaymanagementapi.Client) *Bus {
	return &Bus{client: client}
}

// Send delivers payload to connID. A 410 Gone from the gateway means
// the client disconnected without a clean $disconnect event firing;
// callers receive wsbus.ErrConnGone and should unregister the stale connID.
func (b *Bus) Send(ctx context.Context, connID string, payload []byte) error {
	_, err := b.client.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
		ConnectionId: &connID,
		Data:         payload,
	})
	if err != nil {
		if _, ok := errors.AsType[*apigwtypes.GoneException](err); ok {
			return wsbus.ErrConnGone
		}
		return fmt.Errorf("post to connection %s: %w", connID, err)
	}
	return nil
}
