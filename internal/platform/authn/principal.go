package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/httpx"
)

type Principal struct {
	UserID      string
	Email       string
	Name        string
	SessionID   string
	Roles       []string
	Permissions []string

	permissionSet map[string]struct{}
	roleSet       map[string]struct{}
}

func NewPrincipal(claims *Claims) *Principal {
	p := &Principal{
		UserID:        claims.UserID,
		Email:         claims.Email,
		Name:          claims.Name,
		SessionID:     claims.SessionID,
		Roles:         claims.Roles,
		Permissions:   claims.Permissions,
		permissionSet: make(map[string]struct{}, len(claims.Permissions)),
		roleSet:       make(map[string]struct{}, len(claims.Roles)),
	}
	for _, perm := range claims.Permissions {
		p.permissionSet[perm] = struct{}{}
	}
	for _, role := range claims.Roles {
		p.roleSet[role] = struct{}{}
	}
	return p
}

// Has reports whether the principal holds permission, honouring "*" and
// "resource:*" wildcards.
func (p *Principal) Has(permission string) bool {
	if p == nil {
		return false
	}
	if _, ok := p.permissionSet[permission]; ok {
		return true
	}
	if _, ok := p.permissionSet["*"]; ok {
		return true
	}
	if resource, _, found := strings.Cut(permission, ":"); found {
		if _, ok := p.permissionSet[resource+":*"]; ok {
			return true
		}
	}
	return false
}

func (p *Principal) HasRole(role string) bool {
	if p == nil {
		return false
	}
	_, ok := p.roleSet[role]
	return ok
}

func (p *Principal) IsSuperadmin() bool { return p.HasRole(RoleSuperadmin) }

// RoleSuperadmin is the immutable built-in role that can manage the access matrix.
const RoleSuperadmin = "superadmin"

type principalKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// Authenticate rejects requests without a valid bearer token.
func Authenticate(manager *Manager) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := BearerToken(r.Header.Get("Authorization"))
			if raw == "" {
				httpx.Error(w, r, apierr.Unauthorized("missing bearer token"))
				return
			}
			claims, err := manager.Parse(raw)
			if err != nil {
				httpx.Error(w, r, apierr.Unauthorized("access token is invalid or expired"))
				return
			}
			principal := NewPrincipal(claims)
			httpx.SetUser(r, principal.UserID)
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// Optional authenticates the request when a valid bearer token is present but
// otherwise allows anonymous access. An invalid or expired token is treated as
// anonymous so stale client sessions cannot break public endpoints.
func Optional(manager *Manager) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := BearerToken(r.Header.Get("Authorization"))
			if raw == "" {
				next.ServeHTTP(w, r)
				return
			}
			claims, err := manager.Parse(raw)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			principal := NewPrincipal(claims)
			httpx.SetUser(r, principal.UserID)
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// RequirePermission enforces that the principal holds every listed permission.
func RequirePermission(permissions ...string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := FromContext(r.Context())
			if principal == nil {
				httpx.Error(w, r, apierr.Unauthorized(""))
				return
			}
			for _, permission := range permissions {
				if !principal.Has(permission) {
					httpx.Error(w, r, apierr.Forbidden("missing required permission: "+permission))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSuperadmin restricts a route to the superadmin role.
func RequireSuperadmin() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := FromContext(r.Context())
			if principal == nil || !principal.IsSuperadmin() {
				httpx.Error(w, r, apierr.Forbidden("this action requires the superadmin role"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
