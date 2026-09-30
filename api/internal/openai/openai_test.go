package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

const okAnswer = `{"choices":[{"message":{"content":"{\"verdict\":\"approve\"}"}}],
	"usage":{"prompt_tokens":1200,"completion_tokens":80}}`

var request = review.JSONRequest{
	Model:  "deepseek/deepseek-v4-flash",
	System: "You are a reviewer.",
	Messages: []review.Message{
		{Role: review.RoleUser, Content: "Review this."},
		{Role: review.RoleAssistant, Content: "not json"},
		{Role: review.RoleUser, Content: "Fix it."},
	},
	SchemaName: "Review",
	Schema:     json.RawMessage(`{"type":"object"}`),
	SessionID:  "acme/api#482:General",
}

// recorder keeps the bodies of the requests a test server gets. The server's
// handler runs on other goroutines than the test, so access is locked.
type recorder struct {
	mu       sync.Mutex
	bodies   []map[string]any
	firstHit chan struct{} // closed when the first request arrives
}

func (r *recorder) add(body map[string]any) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bodies = append(r.bodies, body)
	if len(r.bodies) == 1 {
		close(r.firstHit)
	}
	return len(r.bodies)
}

func (r *recorder) requests() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.bodies)
}

// server starts an API that answers each call with the next of answers,
// given as status code and body, and records the requests' bodies.
func server(t *testing.T, answers ...string) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{firstHit: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("got %s %s, want POST /v1/chat/completions", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		n := rec.add(body)

		answer := answers[min(n-1, len(answers)-1)]
		code, text, _ := strings.Cut(answer, " ")
		switch code {
		case "200":
			w.WriteHeader(http.StatusOK)
		case "400":
			w.WriteHeader(http.StatusBadRequest)
		case "429":
			w.WriteHeader(http.StatusTooManyRequests)
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
		}
		io.WriteString(w, text)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// testClient returns a client for srv that doesn't wait long between retries.
func testClient(srv *httptest.Server, openRouter bool) *Client {
	c := NewCompatible(srv.URL+"/v1/", "sk-test")
	c.openRouter = openRouter
	c.retryDelay = time.Millisecond
	return c
}

func TestCompleteJSON(t *testing.T) {
	srv, rec := server(t, "200 "+okAnswer)

	res, err := testClient(srv, false).CompleteJSON(context.Background(), request)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	want := review.JSONResponse{Text: `{"verdict":"approve"}`, TokensIn: 1200, TokensOut: 80}
	if res != want {
		t.Errorf("got %+v, want %+v", res, want)
	}

	body := rec.requests()[0]
	wantBody := `{
		"model": "deepseek/deepseek-v4-flash",
		"messages": [
			{"role": "system", "content": "You are a reviewer."},
			{"role": "user", "content": "Review this."},
			{"role": "assistant", "content": "not json"},
			{"role": "user", "content": "Fix it."}
		],
		"temperature": 0,
		"response_format": {
			"type": "json_schema",
			"json_schema": {"name": "Review", "schema": {"type": "object"}, "strict": true}
		}
	}`
	assertJSON(t, body, wantBody)
}

func TestCompleteJSONOptions(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		openRouter  bool
		temperature bool // whether the body has a temperature
		sessionID   bool // whether the body has session_id and asks for the cost
	}{
		{"OpenAI chat model", "gpt-4.1", false, true, false},
		{"OpenAI reasoning model", "o3-mini", false, false, false},
		{"reasoning model through OpenRouter", "openai/gpt-5", true, false, true},
		{"OpenRouter model", "deepseek/deepseek-v4-flash", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, rec := server(t, "200 "+okAnswer)
			req := request
			req.Model = tt.model
			if _, err := testClient(srv, tt.openRouter).CompleteJSON(context.Background(), req); err != nil {
				t.Fatalf("CompleteJSON: %v", err)
			}
			body := rec.requests()[0]
			if _, ok := body["temperature"]; ok != tt.temperature {
				t.Errorf("temperature sent = %v, want %v", ok, tt.temperature)
			}
			if _, ok := body["session_id"]; ok != tt.sessionID {
				t.Errorf("session_id sent = %v, want %v", ok, tt.sessionID)
			}
			usage, ok := body["usage"]
			if ok != tt.sessionID {
				t.Errorf("usage sent = %v, want %v", ok, tt.sessionID)
			}
			if ok && !reflect.DeepEqual(usage, map[string]any{"include": true}) {
				t.Errorf("usage = %v, want {include: true}", usage)
			}
		})
	}
}

func TestCompleteJSONCost(t *testing.T) {
	tests := []struct {
		name  string
		usage string
		want  *float64
	}{
		{"cost reported", `{"prompt_tokens":1200,"completion_tokens":80,"cost":0.00042}`, ptr(0.00042)},
		{"free model", `{"prompt_tokens":1200,"completion_tokens":80,"cost":0}`, ptr(0.0)},
		{"no cost", `{"prompt_tokens":1200,"completion_tokens":80}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := server(t, `200 {"choices":[{"message":{"content":"{}"}}],"usage":`+tt.usage+`}`)
			res, err := testClient(srv, true).CompleteJSON(context.Background(), request)
			if err != nil {
				t.Fatalf("CompleteJSON: %v", err)
			}
			if !reflect.DeepEqual(res.CostUSD, tt.want) {
				t.Errorf("CostUSD = %v, want %v", deref(res.CostUSD), deref(tt.want))
			}
		})
	}
}

func ptr(f float64) *float64 { return &f }

// deref shows a cost in a test message: its value, or nil.
func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func TestCompleteJSONErrors(t *testing.T) {
	tests := []struct {
		name    string
		answers []string
		calls   int    // requests the client should make
		want    string // part of the error
	}{
		{"server error, then success", []string{"500 oops", "200 " + okAnswer}, 2, ""},
		{"rate limited, then success", []string{"429 slow down", "200 " + okAnswer}, 2, ""},
		{"server error every time", []string{"500 oops"}, 3, "500 Internal Server Error: oops"},
		{"bad request is not retried", []string{`400 {"error":{"message":"Invalid schema"}}`}, 1, "400 Bad Request: Invalid schema"},
		{"no choices, with the provider's error", []string{`200 {"choices":[],"error":{"message":"upstream timeout"}}`}, 1, "returned no answer: upstream timeout"},
		{"answer is not JSON", []string{"200 <html>"}, 1, "decode answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, rec := server(t, tt.answers...)

			_, err := testClient(srv, false).CompleteJSON(context.Background(), request)

			if len(rec.requests()) != tt.calls {
				t.Errorf("made %d requests, want %d", len(rec.requests()), tt.calls)
			}
			if tt.want == "" {
				if err != nil {
					t.Errorf("CompleteJSON: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "sk-test") {
				t.Error("the error contains the API key")
			}
		})
	}
}

func TestCompleteJSONStatusError(t *testing.T) {
	srv, _ := server(t, "400 bad")
	_, err := testClient(srv, false).CompleteJSON(context.Background(), request)

	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusBadRequest {
		t.Errorf("err = %v, want a *StatusError with code 400", err)
	}
}

func TestCompleteJSONStopsWhenCancelled(t *testing.T) {
	srv, rec := server(t, "500 oops")
	c := testClient(srv, false)
	c.retryDelay = time.Hour // the test would hang if the wait ignored ctx

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-rec.firstHit
		cancel()
	}()

	_, err := c.CompleteJSON(ctx, request)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// assertJSON checks that got, a decoded JSON object, equals the JSON in want.
func assertJSON(t *testing.T, got map[string]any, want string) {
	t.Helper()
	var w map[string]any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON: %v", err)
	}
	g, _ := json.MarshalIndent(got, "", "  ")
	wb, _ := json.MarshalIndent(w, "", "  ")
	if string(g) != string(wb) {
		t.Errorf("request body:\n%s\nwant:\n%s", g, wb)
	}
}

func TestModels(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("got %s %s, Authorization %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "" {
			t.Errorf("Content-Type %q on a GET", r.Header.Get("Content-Type"))
		}
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway) // retried
			return
		}
		io.WriteString(w, `{"data": [
			{"id": "gpt-5", "object": "model", "created": 1754000000, "owned_by": "openai"},
			{"id": "deepseek/deepseek-v4-flash", "name": "DeepSeek V4 Flash", "created": 1760000000,
			 "context_length": 163840, "pricing": {"prompt": "0.0000003", "completion": "0.0000012"}}]}`)
	}))
	t.Cleanup(srv.Close)
	c := NewCompatible(srv.URL+"/v1", "sk-test")
	c.retryDelay = time.Millisecond

	got, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{
		{ID: "gpt-5", Created: 1754000000},
		{ID: "deepseek/deepseek-v4-flash", Name: "DeepSeek V4 Flash", Created: 1760000000, ContextLength: 163840,
			Pricing: &Pricing{Prompt: "0.0000003", Completion: "0.0000012"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Models =\n%+v\nwant\n%+v", got, want)
	}
}

func TestVerifyKey(t *testing.T) {
	for _, tt := range []struct {
		status int
		ok     bool
	}{{http.StatusOK, true}, {http.StatusUnauthorized, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/api/v1/key" || r.Header.Get("Authorization") != "Bearer sk-or" {
				t.Errorf("got %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(tt.status)
			io.WriteString(w, `{"data": {}}`)
		}))
		err := NewCompatible(srv.URL+"/api/v1", "sk-or").VerifyKey(context.Background())
		if (err == nil) != tt.ok {
			t.Errorf("status %d: err %v", tt.status, err)
		}
		srv.Close()
	}
}
