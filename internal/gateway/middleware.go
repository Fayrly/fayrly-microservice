package gateway

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/httpx"
	"github.com/fayrly/backend/internal/platform/logging"
)

var allowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions}
var allowedHeaders = []string{"Authorization", "Content-Type", "X-Request-ID", "X-Firebase-Token"}

// CORS answers preflight requests and attaches the access-control headers for
// configured origins. An empty origin list disables cross-origin access; "*"
// allows every origin (safe here because the API authenticates with bearer
// tokens rather than cookies).
func CORS(origins []string, maxAge time.Duration) httpx.Middleware {
	allowed := make(map[string]struct{}, len(origins))
	wildcard := false
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			wildcard = true
			continue
		}
		allowed[strings.TrimSuffix(origin, "/")] = struct{}{}
	}

	permitted := func(origin string) bool {
		if origin == "" {
			return false
		}
		if wildcard {
			return true
		}
		_, ok := allowed[strings.TrimSuffix(origin, "/")]
		return ok
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Add("Vary", "Origin")
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")

			if !permitted(origin) {
				if r.Method == http.MethodOptions {
					httpx.Error(w, r, apierr.Forbidden("origin is not allowed"))
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-RateLimit-Limit, X-RateLimit-Remaining")

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(allowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(int(maxAge.Seconds())))
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is a fixed-rate token bucket per key with lazy refill and periodic
// eviction of idle buckets.
type limiter struct {
	mu          sync.Mutex
	buckets     map[string]*bucket
	rps         float64
	burst       float64
	lastCleanup time.Time
}

func newLimiter(rps, burst int) *limiter {
	return &limiter{
		buckets:     make(map[string]*bucket),
		rps:         float64(rps),
		burst:       float64(burst),
		lastCleanup: time.Now(),
	}
}

// allow reports whether a request may proceed and, when it may not, how long
// the caller should wait before retrying.
func (l *limiter) allow(key string, now time.Time) (bool, float64, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastCleanup) > time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastCleanup = now
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rps)
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, b.tokens, 0
	}

	wait := time.Duration((1 - b.tokens) / l.rps * float64(time.Second))
	if wait <= 0 {
		wait = time.Second
	}
	return false, 0, wait
}

// RateLimiterMiddleware builds the edge rate-limiting middleware. rps is the
// sustained request rate per client, burst the bucket size. The client is keyed
// by the socket address rather than X-Forwarded-For, which the caller controls.
func RateLimiterMiddleware(rps, burst int) httpx.Middleware {
	if rps <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	if burst < 1 {
		burst = rps
	}

	lim := newLimiter(rps, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := remoteIP(r)
			allowed, remaining, retryAfter := lim.allow(key, time.Now())

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rps))
			if allowed {
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(int(math.Floor(remaining))))
				next.ServeHTTP(w, r)
				return
			}

			seconds := int(math.Ceil(retryAfter.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			w.Header().Set("X-RateLimit-Remaining", "0")
			logging.FromContext(r.Context()).Warn("rate limit exceeded",
				"client_ip", key,
				"path", r.URL.Path,
			)
			httpx.Error(w, r, apierr.TooManyRequests(""))
		})
	}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
