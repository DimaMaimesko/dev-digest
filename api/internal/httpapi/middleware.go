package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// securityHeaders adds the headers the TS server gets from helmet. The API
// only serves JSON, so they mostly tell browsers not to reinterpret it.
func securityHeaders(next http.Handler) http.Handler {
	headers := [][2]string{
		{"Content-Security-Policy", "default-src 'self';base-uri 'self';font-src 'self' https: data:;form-action 'self';frame-ancestors 'self';img-src 'self' data:;object-src 'none';script-src 'self';script-src-attr 'none';style-src 'self' https: 'unsafe-inline';upgrade-insecure-requests"},
		{"Cross-Origin-Opener-Policy", "same-origin"},
		{"Cross-Origin-Resource-Policy", "same-origin"},
		{"Origin-Agent-Cluster", "?1"},
		{"Referrer-Policy", "no-referrer"},
		{"Strict-Transport-Security", "max-age=31536000; includeSubDomains"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-DNS-Prefetch-Control", "off"},
		{"X-Download-Options", "noopen"},
		{"X-Frame-Options", "SAMEORIGIN"},
		{"X-Permitted-Cross-Domain-Policies", "none"},
		{"X-XSS-Protection", "0"},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range headers {
			w.Header().Set(h[0], h[1])
		}
		next.ServeHTTP(w, r)
	})
}

// cors lets the web app at origin call the API from the browser, with
// cookies, and answers the browser's preflight requests. It behaves like the
// TS server's @fastify/cors setup.
func cors(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Vary", "Origin")
		h.Set("Access-Control-Allow-Credentials", "true")
		if r.Header.Get("Origin") == origin {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Methods", "GET,HEAD,PUT,PATCH,POST,DELETE")
			if asked := r.Header.Get("Access-Control-Request-Headers"); asked != "" {
				h.Set("Access-Control-Allow-Headers", asked)
			}
			h.Set("Content-Length", "0")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recoverPanics turns a panic in a handler into a 500 response, so one bad
// request can't crash the server.
func recoverPanics(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v) // net/http's signal to drop the connection
				}
				log.Error("handler panicked", "method", r.Method, "path", r.URL.Path, "panic", v)
				writeError(w, http.StatusInternalServerError, "internal_error", "Internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logRequests logs each request's method, path, status and duration.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("request", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "duration", time.Since(start).Round(time.Microsecond))
	})
}

// statusRecorder remembers the status code a handler sends.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the real ResponseWriter, for
// flushing server-sent events later.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
