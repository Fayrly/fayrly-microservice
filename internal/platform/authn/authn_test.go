package authn

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := NewManager(strings.Repeat("k", 32), "fayrly", "fayrly-api", time.Minute)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager
}

func TestNewManagerRejectsWeakConfiguration(t *testing.T) {
	if _, err := NewManager("short", "fayrly", "fayrly-api", time.Minute); err == nil {
		t.Fatal("expected short secret to be rejected")
	}
	if _, err := NewManager(strings.Repeat("k", 32), "", "fayrly-api", time.Minute); err == nil {
		t.Fatal("expected empty issuer to be rejected")
	}
	if _, err := NewManager(strings.Repeat("k", 32), "fayrly", "fayrly-api", 0); err == nil {
		t.Fatal("expected non-positive TTL to be rejected")
	}
}

func TestIssueAndParseRoundTrip(t *testing.T) {
	manager := newTestManager(t)
	token, expiresAt, err := manager.Issue(Identity{
		UserID:      "user-1",
		Email:       "admin@example.com",
		Name:        "Admin",
		Roles:       []string{"admin"},
		Permissions: []string{"user:read", "event:*"},
		SessionID:   "session-1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expiry %v is not in the future", expiresAt)
	}

	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != "user-1" || claims.Subject != "user-1" {
		t.Fatalf("unexpected subject: %+v", claims)
	}
	if claims.Email != "admin@example.com" || claims.SessionID != "session-1" {
		t.Fatalf("unexpected identity claims: %+v", claims)
	}
	if len(claims.Permissions) != 2 || claims.Permissions[1] != "event:*" {
		t.Fatalf("unexpected permissions: %v", claims.Permissions)
	}
}

func TestParseRejectsForeignSecret(t *testing.T) {
	manager := newTestManager(t)
	other, err := NewManager(strings.Repeat("x", 32), "fayrly", "fayrly-api", time.Minute)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	token, _, err := other.Issue(Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := manager.Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestParseRejectsExpiredAndWrongType(t *testing.T) {
	manager := newTestManager(t)
	secret := []byte(strings.Repeat("k", 32))
	past := time.Now().Add(-time.Hour)

	base := func() Claims {
		return Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "fayrly",
				Subject:   "user-1",
				Audience:  jwt.ClaimStrings{"fayrly-api"},
				IssuedAt:  jwt.NewNumericDate(past.Add(-time.Minute)),
				ExpiresAt: jwt.NewNumericDate(past.Add(time.Minute)),
			},
			Type:   TokenTypeAccess,
			UserID: "user-1",
		}
	}

	expired := base()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, expired).SignedString(secret)
	if err != nil {
		t.Fatalf("sign expired: %v", err)
	}
	if _, err := manager.Parse(signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected expired token to fail, got %v", err)
	}

	wrongType := base()
	wrongType.Type = "refresh"
	wrongType.RegisteredClaims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
	signed, err = jwt.NewWithClaims(jwt.SigningMethodHS256, wrongType).SignedString(secret)
	if err != nil {
		t.Fatalf("sign wrong type: %v", err)
	}
	if _, err := manager.Parse(signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected wrong type to fail, got %v", err)
	}
}

func TestBearerToken(t *testing.T) {
	tests := map[string]string{
		"Bearer abc":    "abc",
		"bearer abc":    "abc",
		"Bearer  abc  ": "abc",
		"Basic abc":     "",
		"abc":           "",
		"Bearer":        "",
	}
	for header, want := range tests {
		if got := BearerToken(header); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestPrincipalPermissions(t *testing.T) {
	principal := NewPrincipal(&Claims{
		UserID:      "user-1",
		Roles:       []string{"manager"},
		Permissions: []string{"event:*", "report:read"},
	})
	if !principal.Has("event:write") || !principal.Has("event:publish") {
		t.Fatal("resource wildcard should grant every action")
	}
	if !principal.Has("report:read") {
		t.Fatal("explicit permission should be granted")
	}
	if principal.Has("report:write") {
		t.Fatal("missing permission must be denied")
	}
	if !principal.HasRole("manager") || principal.HasRole("admin") {
		t.Fatal("role lookup is wrong")
	}
	if principal.IsSuperadmin() {
		t.Fatal("manager must not be superadmin")
	}

	superadmin := NewPrincipal(&Claims{Permissions: []string{"*"}, Roles: []string{RoleSuperadmin}})
	if !superadmin.Has("anything:at-all") || !superadmin.IsSuperadmin() {
		t.Fatal("global wildcard and superadmin role must grant access")
	}

	var nilPrincipal *Principal
	if nilPrincipal.Has("event:read") || nilPrincipal.HasRole("admin") || nilPrincipal.IsSuperadmin() {
		t.Fatal("nil principal must deny everything")
	}
}
