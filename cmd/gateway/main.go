// Command gateway is the public entry point for the Fayrly API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fayrly/backend/internal/gateway"
	"github.com/fayrly/backend/internal/platform/config"
	"github.com/fayrly/backend/internal/platform/httpx"
	"github.com/fayrly/backend/internal/platform/logging"
)

func main() {
	logger := logging.New(config.String("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	gw, err := gateway.New(gateway.Config{
		Auth: gateway.Upstream{
			Name: "auth",
			URL:  config.String("AUTH_SERVICE_URL", "http://localhost:8081"),
		},
		User: gateway.Upstream{
			Name: "user",
			URL:  config.String("USER_SERVICE_URL", "http://localhost:8082"),
		},
		Event: gateway.Upstream{
			Name: "event",
			URL:  config.String("EVENT_SERVICE_URL", "http://localhost:8083"),
		},
		Report: gateway.Upstream{
			Name: "report",
			URL:  config.String("REPORT_SERVICE_URL", "http://localhost:8084"),
		},
	}, logger)
	if err != nil {
		return fmt.Errorf("configure gateway: %w", err)
	}

	origins := splitCSV(config.String("CORS_ALLOWED_ORIGINS", ""))
	corsMaxAge := config.Duration("CORS_MAX_AGE", 10*time.Minute)
	rateRPS := config.Int("RATE_LIMIT_RPS", 20)
	rateBurst := config.Int("RATE_LIMIT_BURST", 40)

	handler := httpx.Chain(gw,
		httpx.RequestID(),
		httpx.Recoverer(),
		httpx.AccessLog(logger),
		httpx.Timeout(30*time.Second),
		httpx.SecureHeaders(),
		gateway.CORS(origins, corsMaxAge),
		gateway.RateLimiterMiddleware(rateRPS, rateBurst),
	)

	server := httpx.NewServer(config.String("GATEWAY_ADDR", ":8080"), handler, logger)
	logger.Info("gateway starting",
		"addr", server.Addr,
		"cors_origins", len(origins),
		"rate_limit_rps", rateRPS,
	)
	return httpx.Run(ctx, server, logger)
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
