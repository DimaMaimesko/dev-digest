package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// messagesAPI answers POST /v1/messages with the next of answers ("200
// {...}"), repeating the last, and records the request bodies.
type messagesAPI struct {
	mu      sync.Mutex
	answers []string
	bodies  []map[string]any
}

func (a *messagesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var body map[string]any
	data, _ := io.ReadAll(r.Body)
	json.Unmarshal(data, &body)
	a.bodies = append(a.bodies, body)
	answer := a.answers[min(len(a.bodies)-1, len(a.answers)-1)]
	status, text, _ := strings.Cut(answer, " ")
	w.Header().Set("Content-Type", "application/json")
	switch status {
	case "200":
	case "400":
		w.WriteHeader(http.StatusBadRequest)
	case "500":
		w.WriteHeader(http.StatusInternalServerError)
	}
	io.WriteString(w, text)
}

func fakeMessages(t *testing.T, answers ...string) (*Client, *messagesAPI) {
	t.Helper()
	api := &messagesAPI{answers: answers}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return New(srv.URL, "sk-ant-test"), api
}

const toolAnswer = `200 {"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-4-5",
	"content": [{"type": "text", "text": "Here it is."},
	            {"type": "tool_use", "id": "toolu_1", "name": "Review", "input": {"verdict": "approve", "findings": []}}],
	"stop_reason": "tool_use", "usage": {"input_tokens": 1200, "output_tokens": 80}}`

var reviewRequest = review.JSONRequest{
	Model:  "claude-haiku-4-5",
	System: "You are a reviewer.",
	Messages: []review.Message{
		{Role: review.RoleUser, Content: "Review this."},
		{Role: review.RoleAssistant, Content: ""},
		{Role: review.RoleUser, Content: "Fix it."},
	},
	SchemaName: "Review",
	Schema: json.RawMessage(`{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object",
		"properties": {"verdict": {"type": "string"}}, "required": ["verdict"], "additionalProperties": false}`),
}

func TestCompleteJSON(t *testing.T) {
	c, api := fakeMessages(t, toolAnswer)
	res, err := c.CompleteJSON(context.Background(), reviewRequest)
	if err != nil {
		t.Fatal(err)
	}
	want := review.JSONResponse{Text: `{"verdict": "approve", "findings": []}`, TokensIn: 1200, TokensOut: 80}
	if res != want {
		t.Errorf("response = %+v, want %+v", res, want)
	}

	var got, wantBody any
	b, _ := json.Marshal(api.bodies[0])
	json.Unmarshal(b, &got)
	json.Unmarshal([]byte(`{
		"model": "claude-haiku-4-5", "max_tokens": 4096, "temperature": 0,
		"system": [{"type": "text", "text": "You are a reviewer."}],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Review this."}]},
			{"role": "assistant", "content": [{"type": "text", "text": "(empty answer)"}]},
			{"role": "user", "content": [{"type": "text", "text": "Fix it."}]}],
		"tools": [{"name": "Review", "description": "Return the result as Review.",
			"input_schema": {"$schema": "http://json-schema.org/draft-07/schema#", "type": "object",
				"properties": {"verdict": {"type": "string"}}, "required": ["verdict"], "additionalProperties": false}}],
		"tool_choice": {"type": "tool", "name": "Review"}}`), &wantBody)
	if !reflect.DeepEqual(got, wantBody) {
		t.Errorf("request =\n%s", b)
	}
}

func TestCompleteJSONAnswers(t *testing.T) {
	tests := []struct {
		name, answer, text string
		fails              bool
		calls              int
	}{
		{"no tool call: the text is the answer", `200 {"id": "m", "type": "message", "role": "assistant", "model": "m",
			"content": [{"type": "text", "text": "{\"verdict\": \"approve\"}"}], "stop_reason": "end_turn",
			"usage": {"input_tokens": 1, "output_tokens": 1}}`, `{"verdict": "approve"}`, false, 1},
		{"refused", `200 {"id": "m", "type": "message", "role": "assistant", "model": "m", "content": [],
			"stop_reason": "refusal", "usage": {"input_tokens": 1, "output_tokens": 0}}`, "", true, 1},
		{"bad request isn't retried", `400 {"type": "error", "error": {"type": "invalid_request_error", "message": "bad"}}`, "", true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, api := fakeMessages(t, tt.answer)
			res, err := c.CompleteJSON(context.Background(), reviewRequest)
			if (err != nil) != tt.fails || res.Text != tt.text || len(api.bodies) != tt.calls {
				t.Errorf("text %q, err %v, %d calls", res.Text, err, len(api.bodies))
			}
		})
	}
}

func TestCompleteJSONRetriesServerErrors(t *testing.T) {
	c, api := fakeMessages(t, `500 {"type": "error", "error": {"type": "api_error", "message": "oops"}}`, toolAnswer)
	if _, err := c.CompleteJSON(context.Background(), reviewRequest); err != nil || len(api.bodies) != 2 {
		t.Errorf("err %v after %d calls", err, len(api.bodies))
	}
}

func TestRulesFor(t *testing.T) {
	tests := []struct {
		model                   string
		temperature, forcedTool bool
	}{
		{"claude-haiku-4-5-20251001", true, true},
		{"claude-sonnet-4-6", true, true},
		{"claude-opus-4-6", true, true},
		{"claude-opus-4-7", false, true},
		{"claude-opus-5", false, true},
		{"claude-sonnet-5", false, true},
		{"claude-fable-5", false, true},
		{"claude-opus-5-5", false, false},
		{"claude-fable-5-1", false, false},
		{"claude-mythos-5-1", false, false},
	}
	for _, tt := range tests {
		if r := rulesFor(tt.model); r.temperature != tt.temperature || r.forcedTool != tt.forcedTool {
			t.Errorf("%s: %+v", tt.model, r)
		}
	}
	if got := toolName("My Schema!"); got != "My_Schema_" {
		t.Errorf("toolName = %q", got)
	}
}
