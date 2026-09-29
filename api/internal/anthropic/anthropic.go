// Package anthropic calls Anthropic's Claude API through the official Go SDK.
package anthropic

import (
	"context"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultURL is the base URL of Anthropic's API.
const DefaultURL = "https://api.anthropic.com"

// Client calls Anthropic's API with an API key.
type Client struct {
	sdk sdk.Client
}

// New returns a client for the API at baseURL, such as DefaultURL, that
// authenticates with apiKey. It uses only that key: the SDK's own lookup of
// ANTHROPIC_API_KEY and other credentials is turned off, since DevDigest
// reads its keys from its secrets file first.
func New(baseURL, apiKey string) *Client {
	return &Client{sdk: sdk.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)}
}

// Model is a model the API key can use.
type Model struct {
	ID       string
	Name     string // for people, such as "Claude Opus 5"
	Released time.Time
}

// Models returns every model the API key can use, newest first.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var models []Model
	pages := c.sdk.Models.ListAutoPaging(ctx, sdk.ModelListParams{})
	for pages.Next() {
		m := pages.Current()
		models = append(models, Model{ID: m.ID, Name: m.DisplayName, Released: m.CreatedAt})
	}
	return models, pages.Err()
}
