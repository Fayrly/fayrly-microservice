package event

import (
	"context"
	"net/http"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

// NewRouter wires the event service HTTP API.
func NewRouter(handler *Handler, tokens *authn.Manager, ping func(ctx context.Context) error) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", httpx.HealthHandler("event"))
	mux.HandleFunc("GET /readyz", httpx.ReadinessHandler("event", ping))

	optional := func(fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, authn.Optional(tokens))
	}
	authenticated := func(permissions []string, fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, authn.Authenticate(tokens), authn.RequirePermission(permissions...))
	}
	authenticatedOnly := func(fn http.HandlerFunc) http.Handler {
		return httpx.Chain(fn, authn.Authenticate(tokens))
	}

	mux.Handle("GET /api/v1/events", optional(handler.List))
	mux.Handle("GET /api/v1/events/mine", authenticatedOnly(handler.ListMine))
	mux.Handle("GET /api/v1/events/reviews", optional(handler.ListReviews))
	mux.Handle("POST /api/v1/events/reviews/{id}/helpful", optional(handler.MarkReviewHelpful))
	mux.Handle("GET /api/v1/events/{id}/reviews", optional(handler.ListEventReviews))
	mux.Handle("POST /api/v1/events/{id}/reviews", authenticatedOnly(handler.CreateReview))
	mux.Handle("GET /api/v1/events/{id}", optional(handler.Get))
	mux.Handle("POST /api/v1/events", authenticated([]string{"event:write"}, handler.Create))
	mux.Handle("PATCH /api/v1/events/{id}", authenticated([]string{"event:write"}, handler.Update))
	mux.Handle("POST /api/v1/events/{id}/status", authenticated([]string{"event:publish"}, handler.ChangeStatus))
	mux.Handle("DELETE /api/v1/events/{id}", authenticated([]string{"event:delete"}, handler.Delete))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apierr.NotFound("route"))
	})

	return mux
}
