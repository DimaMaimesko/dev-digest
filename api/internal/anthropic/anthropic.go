// Package anthropic calls Anthropic's Claude API through the official Go SDK.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// DefaultURL is the base URL of Anthropic's API.
const DefaultURL = "https://api.anthropic.com"

// Client calls Anthropic's API with an API key.
type Client struct {
	sdk sdk.Client
}

// Client is an LLM for the review package.
var _ review.LLM = (*Client)(nil)

// New returns a client for the API at baseURL, such as DefaultURL, that
// authenticates with apiKey. It uses only that key: the SDK's own lookup of
// ANTHROPIC_API_KEY and other credentials is turned off, since DevDigest
// reads its keys from its secrets file first.
func New(baseURL, apiKey string) *Client {
	return &Client{sdk: sdk.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(60*time.Second),
		option.WithMaxRetries(3), // rate limits, server and network errors
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

// maxTokens caps an answer's length, as in the TS server.
const maxTokens = 4096

// CompleteJSON asks the model for JSON matching req.Schema. The schema is
// the input of a tool the model must call, as the TS server does; the tool's
// input is the answer.
func (c *Client) CompleteJSON(ctx context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	params, err := request(req)
	if err != nil {
		return review.JSONResponse{}, err
	}
	msg, err := c.sdk.Messages.New(ctx, params)
	if err != nil {
		return review.JSONResponse{}, err
	}
	res := review.JSONResponse{TokensIn: int(msg.Usage.InputTokens), TokensOut: int(msg.Usage.OutputTokens)}
	if msg.StopReason == sdk.StopReasonRefusal {
		return res, fmt.Errorf("%s declined to answer", req.Model)
	}
	var text strings.Builder
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case sdk.ToolUseBlock:
			res.Text = b.JSON.Input.Raw()
			return res, nil
		case sdk.TextBlock:
			text.WriteString(b.Text)
		}
	}
	// No tool call, which happens only when it isn't forced: the JSON, if
	// any, is in the text.
	res.Text = text.String()
	return res, nil
}

// request builds the Messages API request for req.
func request(req review.JSONRequest) (sdk.MessageNewParams, error) {
	var schema map[string]any
	if err := json.Unmarshal(req.Schema, &schema); err != nil {
		return sdk.MessageNewParams{}, fmt.Errorf("schema %s: %w", req.SchemaName, err)
	}
	input := sdk.ToolInputSchemaParam{Properties: schema["properties"], ExtraFields: map[string]any{}}
	for k, v := range schema {
		switch k {
		case "type", "properties":
		case "required":
			for _, r := range v.([]any) {
				name, ok := r.(string)
				if !ok {
					return sdk.MessageNewParams{}, errors.New("schema: required isn't a list of names")
				}
				input.Required = append(input.Required, name)
			}
		default:
			input.ExtraFields[k] = v
		}
	}
	tool := toolName(req.SchemaName)

	params := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: maxTokens,
		Tools: []sdk.ToolUnionParam{{OfTool: &sdk.ToolParam{
			Name:        tool,
			Description: sdk.String("Return the result as " + req.SchemaName + "."),
			InputSchema: input,
		}}},
	}
	if req.System != "" {
		params.System = []sdk.TextBlockParam{{Text: req.System}}
	}
	for _, m := range req.Messages {
		text := m.Content
		if text == "" {
			text = "(empty answer)" // the API refuses an empty text block
		}
		if m.Role == review.RoleAssistant {
			params.Messages = append(params.Messages, sdk.NewAssistantMessage(sdk.NewTextBlock(text)))
		} else {
			params.Messages = append(params.Messages, sdk.NewUserMessage(sdk.NewTextBlock(text)))
		}
	}
	rules := rulesFor(req.Model)
	if rules.temperature {
		params.Temperature = sdk.Float(0)
	}
	if rules.forcedTool {
		params.ToolChoice = sdk.ToolChoiceParamOfTool(tool)
	}
	return params, nil
}

var notToolName = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// toolName makes a schema's name a valid tool name.
func toolName(schema string) string { return notToolName.ReplaceAllString(schema, "_") }

// modelRules say which request options a model accepts.
type modelRules struct {
	temperature bool // a temperature, set to 0 for a steady answer
	forcedTool  bool // tool_choice naming the tool the model must call
}

// rulesFor returns the options model accepts. Per Anthropic's docs, Claude
// Opus 4.7 and later, Sonnet 5, Fable and Mythos refuse sampling
// parameters, and Opus 5.5, Fable 5.1 and Mythos 5.1 refuse a forced tool
// (both with a 400); the TS server sent both to every model. Older models,
// such as Claude Haiku 4.5, get both.
func rulesFor(model string) modelRules {
	r := modelRules{temperature: true, forcedTool: true}
	for _, prefix := range []string{"claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-sonnet-5", "claude-fable-", "claude-mythos-"} {
		if strings.HasPrefix(model, prefix) {
			r.temperature = false
		}
	}
	for _, prefix := range []string{"claude-opus-5-5", "claude-fable-5-1", "claude-mythos-5-1"} {
		if strings.HasPrefix(model, prefix) {
			r.forcedTool = false
		}
	}
	return r
}
