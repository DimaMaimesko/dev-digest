// Package openai calls OpenAI-compatible chat completions APIs: OpenAI,
// OpenRouter, and self-hosted servers such as Ollama or vLLM.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// Client asks models for JSON through a chat completions API.
type Client struct {
	baseURL       string
	apiKey        string
	sendSessionID bool
	http          *http.Client
	retries       int           // extra attempts after a rate limit, server or network error
	retryDelay    time.Duration // wait before the first retry; doubles after each one
}

// Client is an LLM for the review package.
var _ review.LLM = (*Client)(nil)

// New returns a client for the OpenAI API.
func New(apiKey string) *Client {
	return NewCompatible("https://api.openai.com/v1", apiKey)
}

// NewOpenRouter returns a client for OpenRouter. It sends each request's
// SessionID, so the calls of one review are grouped in the OpenRouter
// dashboard. (OpenAI rejects fields it doesn't know, so New doesn't.)
func NewOpenRouter(apiKey string) *Client {
	c := NewCompatible("https://openrouter.ai/api/v1", apiKey)
	c.sendSessionID = true
	return c
}

// NewCompatible returns a client for any OpenAI-compatible API at baseURL,
// such as "http://localhost:11434/v1" for Ollama. The API key may be empty.
func NewCompatible(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey,
		http:       &http.Client{Timeout: 90 * time.Second},
		retries:    2,
		retryDelay: time.Second,
	}
}

// CompleteJSON asks the model for JSON matching req.Schema, using the API's
// strict structured-output mode.
func (c *Client) CompleteJSON(ctx context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	body, err := json.Marshal(c.chatRequest(req))
	if err != nil {
		return review.JSONResponse{}, fmt.Errorf("encode request: %w", err)
	}
	var res chatResponse
	if err := c.post(ctx, body, &res); err != nil {
		return review.JSONResponse{}, err
	}
	if len(res.Choices) == 0 {
		// OpenRouter can answer 200 with no choices and the provider's error
		// in the body.
		msg := "no error message"
		if res.Error != nil {
			msg = res.Error.Message
		}
		return review.JSONResponse{}, fmt.Errorf("%s returned no answer: %s", req.Model, msg)
	}
	return review.JSONResponse{
		Text:      res.Choices[0].Message.Content,
		TokensIn:  res.Usage.PromptTokens,
		TokensOut: res.Usage.CompletionTokens,
	}, nil
}

// chatRequest is the body of POST /chat/completions.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	// A pointer, because 0 must be sent but reasoning models accept no
	// temperature at all.
	Temperature    *float64       `json:"temperature,omitempty"`
	ResponseFormat responseFormat `json:"response_format"`
	SessionID      string         `json:"session_id,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
}

func (c *Client) chatRequest(req review.JSONRequest) chatRequest {
	messages := []chatMessage{{Role: "system", Content: req.System}}
	for _, m := range req.Messages {
		messages = append(messages, chatMessage{Role: string(m.Role), Content: m.Content})
	}
	r := chatRequest{
		Model:    req.Model,
		Messages: messages,
		ResponseFormat: responseFormat{
			Type:       "json_schema",
			JSONSchema: jsonSchema{Name: req.SchemaName, Schema: req.Schema, Strict: true},
		},
	}
	if !reasoningModel(req.Model) {
		zero := 0.0
		r.Temperature = &zero
	}
	if c.sendSessionID {
		r.SessionID = req.SessionID
	}
	return r
}

// reasoningModel reports whether model is an OpenAI reasoning model, which
// rejects a temperature. It accepts OpenRouter IDs such as "openai/o3-mini".
func reasoningModel(model string) bool {
	name := model[strings.LastIndex(model, "/")+1:]
	for _, prefix := range []string{"gpt-5", "o1", "o3", "o4"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// post sends body to /chat/completions and decodes the answer into out. It
// retries rate limits, server errors and network errors, waiting longer each
// time.
func (c *Client) post(ctx context.Context, body []byte, out any) error {
	delay := c.retryDelay
	for attempt := 0; ; attempt++ {
		err := c.postOnce(ctx, body, out)
		if err == nil || attempt == c.retries || !retryable(ctx, err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
}

func (c *Client) postOnce(ctx context.Context, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("read answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &StatusError{Code: resp.StatusCode, Message: errorMessage(data)}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode answer: %w", err)
	}
	return nil
}

// StatusError is an HTTP error from the API.
type StatusError struct {
	Code    int
	Message string // the API's error message, or the start of the response body
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("API returned %d %s: %s", e.Code, http.StatusText(e.Code), e.Message)
}

// errorMessage returns the message of an API error response.
func errorMessage(body []byte) string {
	var res struct {
		Error *apiError `json:"error"`
	}
	if json.Unmarshal(body, &res) == nil && res.Error != nil && res.Error.Message != "" {
		return res.Error.Message
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}

// retryable reports whether trying the same request again may succeed.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false // cancelled, or out of time
	}
	var status *StatusError
	if errors.As(err, &status) {
		return status.Code == http.StatusTooManyRequests || status.Code >= 500
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
