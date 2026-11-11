package report

import (
	"context"
	"net/http"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

// NewRouter wires the report service HTTP API.
func NewRouter(handler *Handler, tokens *authn.Manager, ping func(ctx context.Context) error) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpx.HealthHandler("report"))
	mux.HandleFunc("GET /readyz", httpx.ReadinessHandler("report", ping))

	authenticated := func(permissions []string, fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, authn.Authenticate(tokens), authn.RequirePermission(permissions...))
	}

	mux.Handle("GET /api/v1/reports", authenticated([]string{"report:read"}, handler.List))
	mux.Handle("POST /api/v1/reports", authenticated([]string{"report:write"}, handler.Create))
	mux.Handle("GET /api/v1/reports/{id}", authenticated([]string{"report:read"}, handler.Get))
	mux.Handle("PATCH /api/v1/reports/{id}", authenticated([]string{"report:write"}, handler.Update))
	mux.Handle("DELETE /api/v1/reports/{id}", authenticated([]string{"report:write"}, handler.Delete))
	mux.Handle("POST /api/v1/reports/{id}/submit", authenticated([]string{"report:write"}, handler.Submit))
	mux.Handle("POST /api/v1/reports/{id}/review", authenticated([]string{"report:approve"}, handler.Review))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apierr.NotFound("route"))
	})

	return mux
}
