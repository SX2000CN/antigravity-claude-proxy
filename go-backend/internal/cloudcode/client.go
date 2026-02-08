// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/index.js in the Node.js version.
//
// Cloud Code Client for Antigravity
//
// Communicates with Google's Cloud Code internal API using the
// v1internal:streamGenerateContent endpoint with proper request wrapping.
//
// Supports multi-account load balancing with automatic failover.
//
// Based on: https://github.com/NoeFabris/opencode-antigravity-auth
package cloudcode

import (
	"context"

	"github.com/poemonsense/antigravity-proxy-go/internal/account"
	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/pkg/anthropic"
)

// Client is the main Cloud Code API client
type Client struct {
	accountManager   *account.Manager
	messageHandler   *MessageHandler
	streamingHandler *StreamingHandler
	cfg              *config.Config
}

// NewClient creates a new Cloud Code client
func NewClient(accountManager *account.Manager, cfg *config.Config) *Client {
	return &Client{
		accountManager:   accountManager,
		messageHandler:   NewMessageHandler(accountManager, cfg),
		streamingHandler: NewStreamingHandler(accountManager, cfg),
		cfg:              cfg,
	}
}

// SendMessage sends a non-streaming request to Cloud Code
// Uses SSE endpoint for thinking models (non-streaming doesn't return thinking blocks)
func (c *Client) SendMessage(ctx context.Context, request *anthropic.MessagesRequest, fallbackEnabled bool) (*anthropic.MessagesResponse, error) {
	return c.messageHandler.SendMessage(ctx, request, fallbackEnabled)
}

// SendMessageStream sends a streaming request to Cloud Code
// Streams events in real-time as they arrive from the server
func (c *Client) SendMessageStream(ctx context.Context, request *anthropic.MessagesRequest, fallbackEnabled bool) (<-chan *SSEEvent, <-chan error) {
	return c.streamingHandler.SendMessageStream(ctx, request, fallbackEnabled)
}

// ListModels lists available models in Anthropic API format
func (c *Client) ListModels(ctx context.Context, token string) (*ModelListResponse, error) {
	return ListModels(ctx, token)
}

// FetchAvailableModels fetches available models with quota info from Cloud Code API
func (c *Client) FetchAvailableModels(ctx context.Context, token, projectID string) (*FetchModelsResponse, error) {
	return FetchAvailableModels(ctx, token, projectID)
}

// GetModelQuotas gets model quotas for an account
func (c *Client) GetModelQuotas(ctx context.Context, token, projectID string) (map[string]*ModelQuota, error) {
	return GetModelQuotas(ctx, token, projectID)
}

// GetSubscriptionTier gets subscription tier for an account
func (c *Client) GetSubscriptionTier(ctx context.Context, token string) (*SubscriptionInfo, error) {
	return GetSubscriptionTier(ctx, token)
}

// IsValidModel checks if a model ID is valid
func (c *Client) IsValidModel(ctx context.Context, modelID, token, projectID string) bool {
	return IsValidModel(ctx, modelID, token, projectID)
}
