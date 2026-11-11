package report

import (
	"net/http"
	"strings"

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
	query := r.URL.Query()
	filter := Filter{
		Query:   strings.TrimSpace(query.Get("q")),
		Type:    strings.TrimSpace(query.Get("type")),
		Status:  strings.TrimSpace(query.Get("status")),
		EventID: strings.TrimSpace(query.Get("event_id")),
	}
	if strings.EqualFold(query.Get("mine"), "true") {
		if principal := authn.FromContext(r.Context()); principal != nil {
			filter.CreatedBy = principal.UserID
		}
	}

	reports, total, err := h.service.List(r.Context(), filter, page)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, page, reports, total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	report, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": report})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateReportRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	report, err := h.service.Create(r.Context(), principal, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"data": report})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateReportRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	report, err := h.service.Update(r.Context(), principal, r.PathValue("id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": report})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	principal := authn.FromContext(r.Context())
	if err := h.service.Delete(r.Context(), principal, r.PathValue("id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	principal := authn.FromContext(r.Context())
	report, err := h.service.Submit(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": report})
}

func (h *Handler) Review(w http.ResponseWriter, r *http.Request) {
	var req ReviewRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	report, err := h.service.Review(r.Context(), principal, r.PathValue("id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": report})
}
