// Package authn issues and validates access tokens and exposes the RBAC
// principal used by every service.
package authn

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid token")

const (
	TokenTypeAccess = "access"
	minSecretLength = 32
)

// Identity is the minimum data needed to mint an access token.
type Identity struct {
	UserID      string
	Email       string
	Name        string
	Roles       []string
	Permissions []string
	SessionID   string
}

type Claims struct {
	jwt.RegisteredClaims
	Type        string   `json:"typ"`
	UserID      string   `json:"uid"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"perms"`
	SessionID   string   `json:"sid"`
}

type Manager struct {
	secret    []byte
	issuer    string
	audience  string
	accessTTL time.Duration
}

func NewManager(secret, issuer, audience string, accessTTL time.Duration) (*Manager, error) {
	if len(secret) < minSecretLength {
		return nil, fmt.Errorf("JWT secret must be at least %d characters", minSecretLength)
	}
	if issuer == "" {
		return nil, errors.New("JWT issuer must not be empty")
	}
	if audience == "" {
		return nil, errors.New("JWT audience must not be empty")
	}
	if accessTTL <= 0 {
		return nil, errors.New("access token TTL must be positive")
	}
	return &Manager{secret: []byte(secret), issuer: issuer, audience: audience, accessTTL: accessTTL}, nil
}

func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

func (m *Manager) Issue(identity Identity) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(m.accessTTL)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   identity.UserID,
			Audience:  jwt.ClaimStrings{m.audience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
		},
		Type:        TokenTypeAccess,
		UserID:      identity.UserID,
		Email:       identity.Email,
		Name:        identity.Name,
		Roles:       identity.Roles,
		Permissions: identity.Permissions,
		SessionID:   identity.SessionID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

func (m *Manager) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims,
		func(t *jwt.Token) (any, error) {
			if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, fmt.Errorf("%w: unexpected signing method", ErrInvalidToken)
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithAudience(m.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(15*time.Second),
	)
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Type != TokenTypeAccess {
		return nil, fmt.Errorf("%w: wrong token type", ErrInvalidToken)
	}
	if strings.TrimSpace(claims.UserID) == "" || claims.Subject != claims.UserID {
		return nil, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	return claims, nil
}

// BearerToken extracts the raw token from an Authorization header.
func BearerToken(header string) string {
	scheme, token, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
