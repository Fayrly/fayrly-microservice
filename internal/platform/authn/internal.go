package authn

import (
	"crypto/subtle"
	"net/http"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/httpx"
)

// InternalOnly guards service-to-service endpoints with a shared key.
func InternalOnly(key string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := r.Header.Get("X-Internal-Key")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
				httpx.Error(w, r, apierr.Unauthorized("invalid internal service credentials"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
