package httpapi_test

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
)

// goServer starts the Go API with the fallback pointed at upstream. These
// tests don't touch the database, so it has none.
func goServer(t *testing.T, upstream string) *httptest.Server {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.New(httpapi.Config{WebOrigin: webOrigin, Log: quiet, Fallback: target}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// fakeTS is a stand-in for the TS server. It answers with its own CORS and
// security headers, like the real one, and records what it received.
type fakeTS struct {
	*httptest.Server
	method, path, query, body, origin string
	calls                             int
}

func newFakeTS(t *testing.T) *fakeTS {
	t.Helper()
	f := &fakeTS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.method, f.path, f.query, f.body, f.origin = r.Method, r.URL.Path, r.URL.RawQuery, string(b), r.Header.Get("Origin")
		f.calls++
		w.Header().Set("Access-Control-Allow-Origin", webOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"from": "ts"}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func send(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", webOrigin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestFallbackForwardsUnportedRoutes(t *testing.T) {
	ts := newFakeTS(t)
	api := goServer(t, ts.URL)

	tests := []struct{ name, method, path, body string }{
		{"a GET route not ported", http.MethodGet, "/not-ported/abc?page=2", ""},
		// Only GET /repos is ported; POST /repos still belongs to TS.
		{"another method on a ported path", http.MethodPost, "/repos", `{"url": "https://github.com/o/r"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := send(t, tt.method, api.URL+tt.path, tt.body)
			body, _ := io.ReadAll(res.Body)

			wantPath, wantQuery, _ := strings.Cut(tt.path, "?")
			if ts.method != tt.method || ts.path != wantPath || ts.query != wantQuery || ts.body != tt.body || ts.origin != webOrigin {
				t.Errorf("TS got %s %s?%s %q (Origin %q)", ts.method, ts.path, ts.query, ts.body, ts.origin)
			}
			if res.StatusCode != http.StatusCreated || string(body) != `{"from": "ts"}` {
				t.Errorf("got %d %s, want TS's answer", res.StatusCode, body)
			}
			if got := res.Header.Get("X-Served-By"); got != "ts" {
				t.Errorf("X-Served-By = %q, want ts", got)
			}
		})
	}
}

func TestFallbackKeepsPortedRoutes(t *testing.T) {
	ts := newFakeTS(t)
	res := send(t, http.MethodGet, goServer(t, ts.URL).URL+"/health", "")

	if ts.calls != 0 {
		t.Errorf("TS got %d requests, want none", ts.calls)
	}
	if got := res.Header.Get("X-Served-By"); got != "go" {
		t.Errorf("X-Served-By = %q, want go", got)
	}
}

// Both servers set CORS and security headers; the response must carry one of
// each. Browsers reject a response with two Access-Control-Allow-Origin values.
func TestFallbackHasNoDuplicateHeaders(t *testing.T) {
	res := send(t, http.MethodGet, goServer(t, newFakeTS(t).URL).URL+"/unported", "")
	for _, h := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Vary", "X-Frame-Options", "X-Served-By"} {
		if got := res.Header.Values(h); len(got) != 1 {
			t.Errorf("%s = %q, want one value", h, got)
		}
	}
}

func TestFallbackTSUnreachable(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close() // nothing listens at its address now

	res := send(t, http.MethodGet, goServer(t, down.URL).URL+"/not-ported/abc", "")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusBadGateway ||
		!strings.Contains(string(body), `"code":"upstream_unavailable"`) ||
		!strings.Contains(string(body), "GET /not-ported/abc") {
		t.Errorf("got %d %s", res.StatusCode, body)
	}
}

// A run's live events are a stream (server-sent events). Each event must reach
// the browser when TS sends it, not when the stream ends.
func TestFallbackStreamsEvents(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: info\ndata: {\"msg\": \"Loading PR diff\"}\n\n")
		w.(http.Flusher).Flush()
		<-release // the run goes on
		io.WriteString(w, "event: done\ndata: {}\n\n")
	}))
	defer ts.Close()
	defer close(release)

	api := goServer(t, ts.URL)

	// The whole request runs in a goroutine: without flushing, even the
	// response headers wait for the stream to end, so the deadline must cover
	// sending the request as well as reading the first event.
	first := make(chan string, 1)
	go func() {
		res, err := http.Get(api.URL + "/runs/abc/events")
		if err != nil {
			first <- err.Error()
			return
		}
		defer res.Body.Close()
		line, _ := bufio.NewReader(res.Body).ReadString('\n')
		first <- line
	}()
	select {
	case line := <-first:
		if line != "event: info\n" {
			t.Errorf("first line = %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first event didn't arrive while the stream was still open")
	}
}
