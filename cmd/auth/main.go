// Command auth runs the authentication service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fayrly/backend/internal/auth"
	"github.com/fayrly/backend/internal/auth/userclient"
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
		logger.Error("auth service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if err := config.Required("AUTH_DATABASE_URL", "JWT_SECRET", "INTERNAL_API_KEY", "USER_SERVICE_URL"); err != nil {
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

	db, err := database.Connect(ctx, config.String("AUTH_DATABASE_URL", ""), logger)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	if config.Bool("AUTO_MIGRATE", true) {
		if err := database.Migrate(ctx, db, auth.Migrations, "migrations", logger); err != nil {
			return err
		}
	}

	refreshTTL := config.Duration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	repo := auth.NewRepository(db)
	users := userclient.New(
		config.String("USER_SERVICE_URL", ""),
		config.String("INTERNAL_API_KEY", ""),
		5*time.Second,
	)
	firebase := auth.NewFirebaseVerifier(config.String("FIREBASE_PROJECT_ID", ""))
	mailer := auth.NewMailer(auth.MailerConfig{
		Mode:         config.String("MAIL_MODE", "auto"),
		ResendAPIKey: config.String("RESEND_API_KEY", ""),
		From:         config.String("MAIL_FROM", "Fayrly <no-reply@fayrly.local>"),
		Logger:       logger,
	})
	service := auth.NewService(repo, users, tokens, firebase, mailer, refreshTTL, logger)
	handler := auth.NewHandler(service)
	router := auth.NewRouter(handler, tokens, config.String("INTERNAL_API_KEY", ""), func(ctx context.Context) error { return database.Ping(ctx, db) })

	if email := config.String("BOOTSTRAP_ADMIN_EMAIL", ""); email != "" {
		password := config.String("BOOTSTRAP_ADMIN_PASSWORD", "")
		name := config.String("BOOTSTRAP_ADMIN_NAME", "Platform Owner")
		if password == "" {
			logger.Warn("BOOTSTRAP_ADMIN_EMAIL is set without BOOTSTRAP_ADMIN_PASSWORD; skipping bootstrap")
		} else {
			go bootstrapAdmin(ctx, service, logger, email, password, name)
		}
	}

	go cleanupExpiredTokens(ctx, service, logger)

	server := httpx.NewServer(config.String("AUTH_ADDR", ":8081"), httpx.ApplyDefaults(router, logger), logger)
	logger.Info("auth service starting", "addr", server.Addr, "firebase_enabled", firebase.Enabled(), "mail_enabled", mailer.Enabled())
	return httpx.Run(ctx, server, logger)
}

func bootstrapAdmin(ctx context.Context, service *auth.Service, logger *slog.Logger, email, password, name string) {
	const attempts = 30
	for attempt := 1; attempt <= attempts; attempt++ {
		err := service.BootstrapAdmin(ctx, email, password, name)
		if err == nil {
			logger.Info("bootstrap superadmin is ready", "email", email)
			return
		}
		logger.Warn("bootstrap superadmin attempt failed", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	logger.Error("bootstrap superadmin failed after retries", "email", email)
}

func cleanupExpiredTokens(ctx context.Context, service *auth.Service, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := service.CleanupExpiredTokens(ctx)
			if err != nil {
				logger.Warn("refresh token cleanup failed", "error", err)
				continue
			}
			if deleted > 0 {
				logger.Info("expired refresh tokens removed", "count", deleted)
			}
		}
	}
}
