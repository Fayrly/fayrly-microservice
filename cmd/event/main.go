// Command event runs the concert, charity and community events service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fayrly/backend/internal/event"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/config"
	"github.com/fayrly/backend/internal/platform/database"
	"github.com/fayrly/backend/internal/platform/httpx"
	"github.com/fayrly/backend/internal/platform/logging"
)

func main() {
	logger := logging.New(config.String("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("event service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if err := config.Required("EVENT_DATABASE_URL", "JWT_SECRET"); err != nil {
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

	db, err := database.Connect(ctx, config.String("EVENT_DATABASE_URL", ""), logger)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	if config.Bool("AUTO_MIGRATE", true) {
		if err := database.Migrate(ctx, db, event.Migrations, "migrations", logger); err != nil {
			return err
		}
	}

	repo := event.NewRepository(db)
	service := event.NewService(repo)
	handler := event.NewHandler(service)
	serverKey := config.String("MIDTRANS_SERVER_KEY", "")
	signatureSecret := config.String("MIDTRANS_SIGNATURE_SECRET", "")
	if serverKey != "" || signatureSecret != "" {
		if serverKey == "" || signatureSecret == "" {
			return fmt.Errorf("MIDTRANS_SERVER_KEY and MIDTRANS_SIGNATURE_SECRET must be configured together")
		}
		baseURL := "https://app.sandbox.midtrans.com"
		switch config.String("MIDTRANS_ENVIRONMENT", "sandbox") {
		case "sandbox":
		case "production":
			baseURL = "https://app.midtrans.com"
		default:
			return fmt.Errorf("MIDTRANS_ENVIRONMENT must be sandbox or production")
		}
		handler.SetPayments(event.NewMidtrans(event.MidtransConfig{
			ServerKey: serverKey, SignatureSecret: signatureSecret, BaseURL: baseURL,
		}))
	}
	router := event.NewRouter(handler, tokens, func(ctx context.Context) error { return database.Ping(ctx, db) })

	server := httpx.NewServer(config.String("EVENT_ADDR", ":8083"), httpx.ApplyDefaults(router, logger), logger)
	logger.Info("event service starting", "addr", server.Addr)
	return httpx.Run(ctx, server, logger)
}
