// Command report runs the report service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/config"
	"github.com/fayrly/backend/internal/platform/database"
	"github.com/fayrly/backend/internal/platform/httpx"
	"github.com/fayrly/backend/internal/platform/logging"
	"github.com/fayrly/backend/internal/report"
)

func main() {
	logger := logging.New(config.String("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("report service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if err := config.Required("REPORT_DATABASE_URL", "JWT_SECRET"); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tokens, err := authn.NewManager(
		config.String("JWT_SECRET", ""),
		config.String("JWT_ISSUER", "fayrly"),
		config.String("JWT_AUDIENCE", "fayrly-api"),
		config.Duration("ACCESS_TOKEN_TTL", 15*time.Minute),
	)
	if err != nil {
		return fmt.Errorf("configure tokens: %w", err)
	}

	db, err := database.Connect(ctx, config.String("REPORT_DATABASE_URL", ""), logger)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	if config.Bool("AUTO_MIGRATE", true) {
		if err := database.Migrate(ctx, db, report.Migrations, "migrations", logger); err != nil {
			return err
		}
	}

	repo := report.NewRepository(db)
	service := report.NewService(repo)
	handler := report.NewHandler(service)
	router := report.NewRouter(handler, tokens, func(ctx context.Context) error { return database.Ping(ctx, db) })

	server := httpx.NewServer(config.String("REPORT_ADDR", ":8084"), httpx.ApplyDefaults(router, logger), logger)
	logger.Info("report service starting", "addr", server.Addr)
	return httpx.Run(ctx, server, logger)
}
