package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// servedBy is the response header that says which server answered, "go" or
// "ts". The request log shows it too, so it's easy to see which routes still
// run on the TS server.
const servedBy = "X-Served-By"

// fallbackProxy forwards the requests the Go server doesn't handle yet to the
// TS server at target, so the web app can use the Go server while routes move
// over one by one. A streamed response, such as a run's live events, is passed
// on as it arrives.
func fallbackProxy(target *url.URL, log *slog.Logger) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()
		},
		// The Go middleware has already set these headers on the response.
		// Without removing the TS server's copies, the response would carry
		// two of each, and browsers reject two Access-Control-Allow-Origin
		// values.
		ModifyResponse: func(res *http.Response) error {
			for _, h := range middlewareHeaders {
				res.Header.Del(h)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error("TS server unreachable", "method", r.Method, "path", r.URL.Path, "err", err)
			writeError(w, http.StatusBadGateway, "upstream_unavailable",
				fmt.Sprintf("The Go server doesn't handle %s %s yet, and the TS server at %s didn't answer.", r.Method, r.URL.Path, target))
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(servedBy, "ts")
		proxy.ServeHTTP(w, r)
	})
}

// middlewareHeaders are the response headers the Go middleware sets on every
// response.
var middlewareHeaders = []string{
	"Vary",
	"Access-Control-Allow-Origin",
	"Access-Control-Allow-Credentials",
	"Access-Control-Allow-Methods",
	"Access-Control-Allow-Headers",
	"Content-Security-Policy",
	"Cross-Origin-Opener-Policy",
	"Cross-Origin-Resource-Policy",
	"Origin-Agent-Cluster",
	"Referrer-Policy",
	"Strict-Transport-Security",
	"X-Content-Type-Options",
	"X-DNS-Prefetch-Control",
	"X-Download-Options",
	"X-Frame-Options",
	"X-Permitted-Cross-Domain-Policies",
	"X-XSS-Protection",
}
