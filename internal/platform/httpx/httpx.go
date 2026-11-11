// Package httpx contains HTTP plumbing shared by all services: JSON
// rendering, request decoding, middleware and the server lifecycle.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/logging"
)

const maxBodyBytes = 1 << 20 // 1 MiB

type reqInfoKey struct{}

// ReqInfo is a request-scoped, mutable bag used by middleware and the access
// logger (which runs outside the authentication middleware).
type ReqInfo struct {
	RequestID string
	UserID    string
}

func WithReqInfo(ctx context.Context, info *ReqInfo) context.Context {
	return context.WithValue(ctx, reqInfoKey{}, info)
}

func ReqInfoFromContext(ctx context.Context) *ReqInfo {
	info, _ := ctx.Value(reqInfoKey{}).(*ReqInfo)
	return info
}

func RequestIDFromContext(ctx context.Context) string {
	if info := ReqInfoFromContext(ctx); info != nil {
		return info.RequestID
	}
	return ""
}

// JSON writes a JSON response.
func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if status == http.StatusNoContent || payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Error("encode response", "error", err)
	}
}

func NoContent(w http.ResponseWriter) {
	JSON(w, http.StatusNoContent, nil)
}

type errorDetail struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

// Error renders any error as the standard API error envelope.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := apierr.From(err)
	if apiErr.Status >= http.StatusInternalServerError {
		logging.FromContext(r.Context()).Error("request failed",
			"code", apiErr.Code,
			"error", apiErr.Error(),
			"method", r.Method,
			"path", r.URL.Path,
		)
	}
	JSON(w, apiErr.Status, errorBody{Error: errorDetail{
		Code:      apiErr.Code,
		Message:   apiErr.Message,
		Details:   apiErr.Details,
		RequestID: RequestIDFromContext(r.Context()),
	}})
}

// Bind decodes a JSON request body strictly, enforcing a 1 MiB limit.
func Bind(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return apierr.New(http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var maxErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return apierr.BadRequest("request body is required")
		case errors.As(err, &syntaxErr):
			return apierr.BadRequest("request body contains malformed JSON")
		case errors.As(err, &typeErr):
			return apierr.Validation(map[string]string{typeErr.Field: fmt.Sprintf("must be of type %s", typeErr.Type)})
		case errors.As(err, &maxErr):
			return apierr.New(http.StatusRequestEntityTooLarge, "payload_too_large", "request body must not exceed 1 MiB")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
			return apierr.Validation(map[string]string{field: "is not a recognized field"})
		default:
			return apierr.BadRequest("request body could not be decoded")
		}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apierr.BadRequest("request body must contain a single JSON object")
	}
	return nil
}

// Page is a validated pagination request.
type Page struct {
	Number int
	Size   int
}

func ParsePage(r *http.Request) Page {
	number := 1
	size := 20
	if raw := r.URL.Query().Get("page"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			number = v
		}
	}
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			size = v
		}
	}
	if size > 100 {
		size = 100
	}
	return Page{Number: number, Size: size}
}

func (p Page) Offset() int { return (p.Number - 1) * p.Size }

type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

func NewPagination(page Page, total int) Pagination {
	pages := 0
	if total > 0 {
		pages = (total + page.Size - 1) / page.Size
	}
	return Pagination{Page: page.Number, PageSize: page.Size, Total: total, TotalPages: pages}
}

type ListResponse struct {
	Data       any        `json:"data"`
	Pagination Pagination `json:"pagination"`
}

func List(w http.ResponseWriter, page Page, items any, total int) {
	JSON(w, http.StatusOK, ListResponse{Data: items, Pagination: NewPagination(page, total)})
}

// HealthHandler returns a liveness handler.
func HealthHandler(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": service})
	}
}

// ReadinessHandler returns a readiness handler that pings a dependency.
func ReadinessHandler(service string, ping func(ctx context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ping(ctx); err != nil {
			logging.FromContext(r.Context()).Warn("readiness check failed", "error", err)
			JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "service": service})
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ready", "service": service})
	}
}

// ClientIP resolves the originating address for rate limiting.
func ClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// NewRequestID returns a new UUID request identifier.
func NewRequestID() string { return uuid.NewString() }
