package auth

import (
	"context"
	"net/http"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

// NewRouter wires the auth service HTTP API.
func NewRouter(handler *Handler, tokens *authn.Manager, internalKey string, ping func(ctx context.Context) error) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpx.HealthHandler("auth"))
	mux.HandleFunc("GET /readyz", httpx.ReadinessHandler("auth", ping))

	authenticated := func(fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, authn.Authenticate(tokens))
	}
	internal := func(fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, authn.InternalOnly(internalKey))
	}

	mux.HandleFunc("POST /api/v1/auth/register", handler.Register)
	mux.HandleFunc("POST /api/v1/auth/login", handler.Login)
	mux.HandleFunc("POST /api/v1/auth/firebase", handler.Firebase)
	mux.HandleFunc("POST /api/v1/auth/refresh", handler.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", handler.Logout)
	mux.HandleFunc("POST /api/v1/auth/password/forgot", handler.ForgotPassword)
	mux.HandleFunc("POST /api/v1/auth/password/verify", handler.VerifyResetCode)
	mux.HandleFunc("POST /api/v1/auth/password/reset", handler.ResetPasswordWithCode)
	mux.Handle("GET /api/v1/auth/me", authenticated(handler.Me))
	mux.Handle("POST /api/v1/auth/password", authenticated(handler.ChangePassword))

	mux.Handle("POST /internal/credentials", internal(handler.CreateCredential))
	mux.Handle("DELETE /internal/credentials/{id}", internal(handler.DeleteCredential))
	mux.Handle("POST /internal/credentials/{id}/reset-password", internal(handler.ResetPassword))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apierr.NotFound("route"))
	})

	return mux
}
