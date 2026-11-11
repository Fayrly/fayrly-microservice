package auth

import (
	"net/http"

	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	session, err := h.service.Register(r.Context(), req, requestMeta(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, session)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	session, err := h.service.Login(r.Context(), req, requestMeta(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

func (h *Handler) Firebase(w http.ResponseWriter, r *http.Request) {
	var req FirebaseRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	session, err := h.service.FirebaseLogin(r.Context(), req, requestMeta(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	session, err := h.service.Refresh(r.Context(), req.RefreshToken, requestMeta(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	if err := h.service.Logout(r.Context(), req.RefreshToken); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	principal := authn.FromContext(r.Context())
	profile, err := h.service.Me(r.Context(), principal.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user": profile})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	principal := authn.FromContext(r.Context())
	if err := h.service.ChangePassword(r.Context(), principal.UserID, req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	if err := h.service.RequestPasswordReset(r.Context(), req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
}

func (h *Handler) VerifyResetCode(w http.ResponseWriter, r *http.Request) {
	var req VerifyResetCodeRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	if err := h.service.VerifyResetCode(r.Context(), req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ResetPasswordWithCode(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	if err := h.service.ResetPasswordWithCode(r.Context(), req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	var req CreateCredentialRequest
	if !httpx.BindValid(w, r, &req) {
		return
	}
	response, err := h.service.CreateCredential(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, response)
}

func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteCredential(r.Context(), r.PathValue("id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	response, err := h.service.ResetPassword(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, response)
}

func requestMeta(r *http.Request) RequestMeta {
	userAgent := r.UserAgent()
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	return RequestMeta{UserAgent: userAgent, IP: httpx.ClientIP(r)}
}
