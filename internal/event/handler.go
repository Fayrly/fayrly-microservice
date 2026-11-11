package event

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	filter, err := parseFilter(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	events, total, err := h.service.List(r.Context(), authn.FromContext(r.Context()), filter, page)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, page, events, total)
}

func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	filter, err := parseFilter(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	principal := authn.FromContext(r.Context())
	events, total, err := h.service.ListMine(r.Context(), principal, filter, page)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, page, events, total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	event, err := h.service.Get(r.Context(), authn.FromContext(r.Context()), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": event})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateEventRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	event, err := h.service.Create(r.Context(), principal, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"data": event})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateEventRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	event, err := h.service.Update(r.Context(), principal, r.PathValue("id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": event})
}

func (h *Handler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	var req StatusRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	event, err := h.service.ChangeStatus(r.Context(), principal, r.PathValue("id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": event})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), r.PathValue("id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ListReviews(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	reviews, total, err := h.service.ListReviews(r.Context(), page)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, page, reviews, total)
}

func (h *Handler) ListEventReviews(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	principal := authn.FromContext(r.Context())
	reviews, summary, err := h.service.ListEventReviews(r.Context(), principal, r.PathValue("id"), page)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": reviews, "summary": summary})
}

func (h *Handler) CreateReview(w http.ResponseWriter, r *http.Request) {
	var req CreateReviewRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	review, err := h.service.CreateReview(r.Context(), principal, r.PathValue("id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"data": review})
}

func (h *Handler) MarkReviewHelpful(w http.ResponseWriter, r *http.Request) {
	count, err := h.service.MarkReviewHelpful(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"helpful_count": count})
}

func parseFilter(r *http.Request) (Filter, error) {
	query := r.URL.Query()
	filter := Filter{
		Query: strings.TrimSpace(query.Get("q")),
		Type:  strings.TrimSpace(query.Get("type")),
		City:  strings.TrimSpace(query.Get("city")),
	}
	if status := strings.TrimSpace(query.Get("status")); status != "" {
		filter.Statuses = []string{status}
	}

	from, err := parseTimeParam(query.Get("from"))
	if err != nil {
		return filter, apierr.Validation(map[string]string{"from": err.Error()})
	}
	to, err := parseTimeParam(query.Get("to"))
	if err != nil {
		return filter, apierr.Validation(map[string]string{"to": err.Error()})
	}
	filter.StartsFrom = from
	filter.StartsTo = to
	return filter, nil
}

func parseTimeParam(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return &parsed, nil
		}
	}
	return nil, errors.New("must be an RFC3339 timestamp or a YYYY-MM-DD date")
}
