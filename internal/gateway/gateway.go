// Package gateway terminates client traffic and routes it to the internal
// services. It owns cross-cutting edge concerns: CORS, rate limiting and
// aggregated readiness.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/httpx"
)

// Upstream describes one internal service the gateway proxies to.
type Upstream struct {
	Name string
	URL  string
}

// Config lists the upstream services.
type Config struct {
	Auth   Upstream
	User   Upstream
	Event  Upstream
	Report Upstream
}

type route struct {
	prefix string
	proxy  http.Handler
}

type target struct {
	name   string
	base   *url.URL
	client *http.Client
}

// Gateway is the public HTTP entry point.
type Gateway struct {
	logger  *slog.Logger
	routes  []route
	targets []target
	client  *http.Client
}

// New builds a gateway that forwards each API prefix to its owning service.
func New(cfg Config, logger *slog.Logger) (*Gateway, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 25 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
	}

	g := &Gateway{
		logger: logger,
		client: &http.Client{Transport: transport, Timeout: 5 * time.Second},
	}

	upstreams := []struct {
		upstream Upstream
		prefixes []string
	}{
		{cfg.Auth, []string{"/api/v1/auth"}},
		{cfg.User, []string{"/api/v1/users", "/api/v1/roles", "/api/v1/permissions", "/api/v1/access-matrix"}},
		{cfg.Event, []string{"/api/v1/events"}},
		{cfg.Report, []string{"/api/v1/reports"}},
	}

	for _, entry := range upstreams {
		base, err := url.Parse(entry.upstream.URL)
		if err != nil {
			return nil, fmt.Errorf("parse %s upstream url: %w", entry.upstream.Name, err)
		}
		if base.Scheme != "http" && base.Scheme != "https" {
			return nil, fmt.Errorf("%s upstream url must use http or https", entry.upstream.Name)
		}
		if base.Host == "" {
			return nil, fmt.Errorf("%s upstream url is missing a host", entry.upstream.Name)
		}

		proxy := newProxy(entry.upstream.Name, base, transport, logger)
		g.targets = append(g.targets, target{name: entry.upstream.Name, base: base, client: g.client})
		for _, prefix := range entry.prefixes {
			g.routes = append(g.routes, route{prefix: prefix, proxy: proxy})
		}
	}

	return g, nil
}

// newProxy builds a reverse proxy that forwards the original path unchanged and
// renders upstream failures using the shared error envelope.
func newProxy(name string, base *url.URL, transport http.RoundTripper, logger *slog.Logger) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(base)
	proxy.Transport = transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}

		apiErr := apierr.New(http.StatusBadGateway, "bad_gateway", name+" service is unreachable")
		if errors.Is(err, context.DeadlineExceeded) {
			apiErr = apierr.New(http.StatusGatewayTimeout, "gateway_timeout", name+" service timed out")
		}

		logger.Error("upstream request failed",
			"upstream", name,
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)
		httpx.Error(w, r, apiErr)
	}

	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		if r.Header.Get("X-Forwarded-Host") == "" {
			r.Header.Set("X-Forwarded-Host", r.Host)
		}
		if r.Header.Get("X-Forwarded-Proto") == "" {
			if r.TLS != nil {
				r.Header.Set("X-Forwarded-Proto", "https")
			} else {
				r.Header.Set("X-Forwarded-Proto", "http")
			}
		}
	}

	return proxy
}

// ServeHTTP dispatches to the owning service or answers health probes and the
// JSON 404 used for unknown paths.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		httpx.HealthHandler("gateway")(w, r)
		return
	case "/readyz":
		g.readiness(w, r)
		return
	}

	for _, rt := range g.routes {
		if rt.matches(r.URL.Path) {
			rt.proxy.ServeHTTP(w, r)
			return
		}
	}

	httpx.Error(w, r, apierr.NotFound("route"))
}

func (rt route) matches(path string) bool {
	return path == rt.prefix || strings.HasPrefix(path, rt.prefix+"/")
}

type readinessResult struct {
	Status   string            `json:"status"`
	Services map[string]string `json:"services"`
}

// readiness fans out to every upstream /readyz and reports the aggregate.
func (g *Gateway) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	results := make(map[string]string, len(g.targets))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, t := range g.targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			state := g.probe(ctx, t)
			mu.Lock()
			results[t.name] = state
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	body := readinessResult{Status: "ok", Services: results}
	status := http.StatusOK
	for _, state := range results {
		if state != "ok" {
			body.Status = "degraded"
			status = http.StatusServiceUnavailable
			break
		}
	}

	httpx.JSON(w, status, body)
}

func (g *Gateway) probe(ctx context.Context, t target) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base.String()+"/readyz", nil)
	if err != nil {
		return "error"
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "unreachable"
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusOK {
		return "unhealthy"
	}
	return "ok"
}
