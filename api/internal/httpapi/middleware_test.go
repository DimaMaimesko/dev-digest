package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoverPanics(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })

	rec := httptest.NewRecorder()
	recoverPanics(log, boom).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "boom") || !strings.Contains(body, `"internal_error"`) {
		t.Errorf("body = %s, want the error envelope without the panic value", body)
	}
	if !strings.Contains(logs.String(), "panic=boom") {
		t.Errorf("the panic was not logged: %s", logs.String())
	}
}

func TestLogRequests(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	teapot := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })

	logRequests(log, teapot).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tea", nil))

	if line := logs.String(); !strings.Contains(line, "method=GET path=/tea status=418 by=go") {
		t.Errorf("log = %q", line)
	}
}
