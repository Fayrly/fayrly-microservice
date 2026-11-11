package user

import (
	"net/http"
	"strings"

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

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	filter := UserFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Status: strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))),
		Role:   strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role"))),
	}
	users, total, err := h.service.ListUsers(r.Context(), filter, page)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, page, users, total)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	response, err := h.service.CreateUser(r.Context(), principal, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, response)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.service.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": u})
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var req UpdateUserRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	u, err := h.service.UpdateUser(r.Context(), principal, r.PathValue("id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": u})
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	principal := authn.FromContext(r.Context())
	if err := h.service.DeleteUser(r.Context(), principal, r.PathValue("id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) AssignRoles(w http.ResponseWriter, r *http.Request) {
	var req AssignRolesRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	u, err := h.service.AssignRoles(r.Context(), principal, r.PathValue("id"), req.Roles)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": u})
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	response, err := h.service.ResetPassword(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, response)
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.service.ListRoles(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": roles})
}

func (h *Handler) GetRole(w http.ResponseWriter, r *http.Request) {
	role, err := h.service.GetRole(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": role})
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var req CreateRoleRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	role, err := h.service.CreateRole(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"data": role})
}

func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	var req UpdateRoleRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	role, err := h.service.UpdateRole(r.Context(), r.PathValue("id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": role})
}

func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteRole(r.Context(), r.PathValue("id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) SetRolePermissions(w http.ResponseWriter, r *http.Request) {
	var req SetPermissionsRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	role, err := h.service.SetRolePermissions(r.Context(), r.PathValue("id"), req.Permissions)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": role})
}

func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	permissions, err := h.service.ListPermissions(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": permissions})
}

func (h *Handler) AccessMatrix(w http.ResponseWriter, r *http.Request) {
	matrix, err := h.service.AccessMatrix(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, matrix)
}

func (h *Handler) SetAccessMatrix(w http.ResponseWriter, r *http.Request) {
	var req AccessMatrixRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	matrix, err := h.service.SetAccessMatrix(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, matrix)
}

// --- internal -------------------------------------------------------------

func (h *Handler) EnsureUser(w http.ResponseWriter, r *http.Request) {
	var req EnsureUserRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	u, err := h.service.EnsureUser(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

func (h *Handler) GetUserByEmailInternal(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" {
		httpx.Error(w, r, apierr.BadRequest("email query parameter is required"))
		return
	}
	u, err := h.service.GetUserByEmailInternal(r.Context(), email)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

func (h *Handler) GetUserInternal(w http.ResponseWriter, r *http.Request) {
	u, err := h.service.GetUserInternal(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}
