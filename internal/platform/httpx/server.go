package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// NewServer builds an http.Server with defensive timeouts.
func NewServer(addr string, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

// ApplyDefaults wraps a service router with the standard middleware stack.
func ApplyDefaults(handler http.Handler, logger *slog.Logger) http.Handler {
	return Chain(handler,
		RequestID(),
		Recoverer(),
		AccessLog(logger),
		Timeout(20*time.Second),
		SecureHeaders(),
	)
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func Run(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("shutting down http server")
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
