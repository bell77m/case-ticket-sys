package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ticket-app/internal/api"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(&api.Server{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want %d", rec.Code, http.StatusOK)
	}
}

// Unknown API paths must be JSON-style 404s, never the SPA's index.html.
func TestUnknownAPIPathIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux(&api.Server{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/nope = %d, want 404", rec.Code)
	}
}

// Every response carries the default security headers, API and SPA alike (T2.14).
func TestSecurityHeaders(t *testing.T) {
	h := newMux(&api.Server{})
	for _, path := range []string{"/", "/healthz", "/api/nope"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		got := rec.Header()
		if got.Get("X-Content-Type-Options") != "nosniff" || got.Get("X-Frame-Options") != "DENY" ||
			got.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(got.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("GET %s headers = %v, want nosniff, DENY, no-referrer and frame-ancestors 'none'", path, got)
		}
	}
}

// A client that trickles a request or holds an idle connection is cut off (T2.14). No WriteTimeout: evidence
// downloads and server-sent events (T3.04) run long on purpose.
func TestServerTimeouts(t *testing.T) {
	srv := newServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.IdleTimeout <= 0 || srv.WriteTimeout != 0 {
		t.Errorf("timeouts: header %v read %v idle %v write %v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout, srv.WriteTimeout)
	}
}
