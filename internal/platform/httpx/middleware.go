package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/logging"
)

type Middleware func(http.Handler) http.Handler

func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

// RequestID assigns (or forwards) a request identifier and stores it in the
// request-scoped ReqInfo so the access log can include it.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" || len(id) > 128 {
				id = NewRequestID()
			}
			info := &ReqInfo{RequestID: id}
			ctx := WithReqInfo(r.Context(), info)
			ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With("request_id", id))

			r = r.WithContext(ctx)
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r)
		})
	}
}

// Recoverer converts panics into JSON 500 responses.
func Recoverer() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logging.FromContext(r.Context()).Error("panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
					)
					Error(w, r, apierr.Internal(nil))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout bounds the request context. Handlers and database calls observe the
// deadline and return an error, which the error envelope renders.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(status int) {
	if s.status == 0 {
		s.status = status
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// AccessLog records one structured line per request.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= http.StatusInternalServerError {
				level = slog.LevelError
			} else if status >= http.StatusBadRequest {
				level = slog.LevelWarn
			}

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", rec.bytes,
				"remote_ip", ClientIP(r),
			}
			if info := ReqInfoFromContext(r.Context()); info != nil {
				attrs = append(attrs, "request_id", info.RequestID)
				if info.UserID != "" {
					attrs = append(attrs, "user_id", info.UserID)
				}
			}
			logger.Log(r.Context(), level, "http request", attrs...)
		})
	}
}

// SetUser marks the authenticated principal on the request info.
func SetUser(r *http.Request, userID string) {
	if info := ReqInfoFromContext(r.Context()); info != nil {
		info.UserID = userID
	}
}

// SecureHeaders adds conservative security headers.
func SecureHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			next.ServeHTTP(w, r)
		})
	}
}
