// Command user runs the user, role and access-matrix service.
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
	"github.com/fayrly/backend/internal/user"
	"github.com/fayrly/backend/internal/user/authclient"
)

func main() {
	logger := logging.New(config.String("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("user service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if err := config.Required("USER_DATABASE_URL", "JWT_SECRET", "INTERNAL_API_KEY", "AUTH_SERVICE_URL"); err != nil {
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

	db, err := database.Connect(ctx, config.String("USER_DATABASE_URL", ""), logger)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	if config.Bool("AUTO_MIGRATE", true) {
		if err := database.Migrate(ctx, db, user.Migrations, "migrations", logger); err != nil {
			return err
		}
	}

	repo := user.NewRepository(db)
	auth := authclient.New(
		config.String("AUTH_SERVICE_URL", ""),
		config.String("INTERNAL_API_KEY", ""),
		5*time.Second,
	)
	service := user.NewService(repo, auth, logger)
	handler := user.NewHandler(service)
	router := user.NewRouter(handler, tokens, config.String("INTERNAL_API_KEY", ""), func(ctx context.Context) error { return database.Ping(ctx, db) })

	server := httpx.NewServer(config.String("USER_ADDR", ":8082"), httpx.ApplyDefaults(router, logger), logger)
	logger.Info("user service starting", "addr", server.Addr)
	return httpx.Run(ctx, server, logger)
}
