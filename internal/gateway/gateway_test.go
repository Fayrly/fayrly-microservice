package gateway

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRoutingByPrefix(t *testing.T) {
	echo := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "%s:%s", name, r.URL.Path)
		}))
	}
	authSrv := echo("auth")
	userSrv := echo("user")
	eventSrv := echo("event")
	reportSrv := echo("report")
	defer authSrv.Close()
	defer userSrv.Close()
	defer eventSrv.Close()
	defer reportSrv.Close()

	gw, err := New(Config{
		Auth:   Upstream{Name: "auth", URL: authSrv.URL},
		User:   Upstream{Name: "user", URL: userSrv.URL},
		Event:  Upstream{Name: "event", URL: eventSrv.URL},
		Report: Upstream{Name: "report", URL: reportSrv.URL},
	}, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/auth/login", "auth:/api/v1/auth/login"},
		{"/api/v1/auth/me", "auth:/api/v1/auth/me"},
		{"/api/v1/users/123/roles", "user:/api/v1/users/123/roles"},
		{"/api/v1/access-matrix", "user:/api/v1/access-matrix"},
		{"/api/v1/events/slug/hello", "event:/api/v1/events/slug/hello"},
		{"/api/v1/reports/1/submit", "report:/api/v1/reports/1/submit"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		rec := httptest.NewRecorder()
		gw.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tt.path, rec.Code)
		}
		if got := rec.Body.String(); got != tt.want {
			t.Errorf("%s routed to %q, want %q", tt.path, got, tt.want)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/authx", nil)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown prefix returned %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("unknown prefix body = %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/internal/users/by-email", nil)
	rec = httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("internal route leaked through the gateway: %d", rec.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	gw, err := New(Config{
		Auth:   Upstream{Name: "auth", URL: "http://127.0.0.1:1"},
		User:   Upstream{Name: "user", URL: "http://127.0.0.1:1"},
		Event:  Upstream{Name: "event", URL: "http://127.0.0.1:1"},
		Report: Upstream{Name: "report", URL: "http://127.0.0.1:1"},
	}, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"service":"gateway"`) {
		t.Fatalf("healthz body = %s", rec.Body.String())
	}
}

func TestCORSPreflight(t *testing.T) {
	handler := CORS([]string{"http://localhost:3000"}, 10*time.Minute)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("allow-origin = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("allow-headers = %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}

	req = httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disallowed preflight status = %d, want 403", rec.Code)
	}
}

func TestRateLimiter(t *testing.T) {
	lim := newLimiter(2, 2)
	start := time.Unix(1000, 0)

	if allowed, _, _ := lim.allow("a", start); !allowed {
		t.Fatal("first request should pass")
	}
	if allowed, _, _ := lim.allow("a", start); !allowed {
		t.Fatal("second request should pass")
	}
	allowed, _, retry := lim.allow("a", start)
	if allowed {
		t.Fatal("third request should be limited")
	}
	if retry <= 0 || retry > 500*time.Millisecond {
		t.Fatalf("retry after = %v", retry)
	}
	if allowed, _, _ := lim.allow("b", start); !allowed {
		t.Fatal("another key must have its own bucket")
	}
	if allowed, _, _ := lim.allow("a", start.Add(500*time.Millisecond)); !allowed {
		t.Fatal("bucket should refill after half a second")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	handler := RateLimiterMiddleware(1, 1)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("429 response must include Retry-After")
	}
	if !strings.Contains(second.Body.String(), "rate_limited") {
		t.Fatalf("429 body = %s", second.Body.String())
	}
}
