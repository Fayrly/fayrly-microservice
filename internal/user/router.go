package user

import (
	"context"
	"net/http"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

// NewRouter wires the user service HTTP API.
func NewRouter(handler *Handler, tokens *authn.Manager, internalKey string, ping func(ctx context.Context) error) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpx.HealthHandler("user"))
	mux.HandleFunc("GET /readyz", httpx.ReadinessHandler("user", ping))

	authenticated := func(permissions []string, fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn,
			authn.Authenticate(tokens),
			authn.RequirePermission(permissions...),
		)
	}
	internal := func(fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, authn.InternalOnly(internalKey))
	}

	mux.Handle("GET /api/v1/users", authenticated([]string{"user:read"}, handler.ListUsers))
	mux.Handle("POST /api/v1/users", authenticated([]string{"user:write"}, handler.CreateUser))
	mux.Handle("GET /api/v1/users/{id}", authenticated([]string{"user:read"}, handler.GetUser))
	mux.Handle("PATCH /api/v1/users/{id}", authenticated([]string{"user:write"}, handler.UpdateUser))
	mux.Handle("DELETE /api/v1/users/{id}", authenticated([]string{"user:delete"}, handler.DeleteUser))
	mux.Handle("PUT /api/v1/users/{id}/roles", authenticated([]string{"user:write"}, handler.AssignRoles))
	mux.Handle("POST /api/v1/users/{id}/reset-password", authenticated([]string{"user:write"}, handler.ResetPassword))

	mux.Handle("GET /api/v1/roles", authenticated([]string{"role:read"}, handler.ListRoles))
	mux.Handle("POST /api/v1/roles", authenticated([]string{"role:write"}, handler.CreateRole))
	mux.Handle("GET /api/v1/roles/{id}", authenticated([]string{"role:read"}, handler.GetRole))
	mux.Handle("PATCH /api/v1/roles/{id}", authenticated([]string{"role:write"}, handler.UpdateRole))
	mux.Handle("DELETE /api/v1/roles/{id}", authenticated([]string{"role:write"}, handler.DeleteRole))
	mux.Handle("PUT /api/v1/roles/{id}/permissions", authenticated([]string{"role:write"}, handler.SetRolePermissions))

	mux.Handle("GET /api/v1/permissions", authenticated([]string{"role:read"}, handler.ListPermissions))
	mux.Handle("GET /api/v1/access-matrix", authenticated([]string{"role:read"}, handler.AccessMatrix))
	mux.Handle("PUT /api/v1/access-matrix", authenticated([]string{"role:write"}, handler.SetAccessMatrix))

	mux.Handle("POST /internal/users/ensure", internal(handler.EnsureUser))
	mux.Handle("GET /internal/users/by-email", internal(handler.GetUserByEmailInternal))
	mux.Handle("GET /internal/users/{id}", internal(handler.GetUserInternal))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apierr.NotFound("route"))
	})

	return mux
}
