package review

import (
	"context"
	"encoding/json"
)

// LLM is a language model that answers with JSON matching a schema.
//
// Adapters for OpenAI, Anthropic and OpenRouter implement it. Each call is one
// request to the provider. An adapter may retry network errors, but not an
// answer that doesn't fit the schema: that is handled once, in this package.
type LLM interface {
	CompleteJSON(ctx context.Context, req JSONRequest) (JSONResponse, error)
}

// JSONRequest asks a model for a JSON value that matches Schema.
type JSONRequest struct {
	Model      string
	System     string
	Messages   []Message       // the conversation after the system prompt, oldest first
	SchemaName string          // names the schema for the provider, e.g. "Review"
	Schema     json.RawMessage // a JSON Schema, draft-07
	SessionID  string          // optional; OpenRouter groups calls with the same ID
}

// Role says who wrote a message.
type Role string

// Roles of the messages in a conversation. The system prompt is not a message;
// it goes in JSONRequest.System.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of a conversation with a model.
type Message struct {
	Role    Role
	Content string
}

// JSONResponse is a model's answer to a JSONRequest.
type JSONResponse struct {
	Text      string // the JSON as the model wrote it; not yet checked against the schema
	TokensIn  int
	TokensOut int
}
