package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const firebaseCertsURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"

type FirebaseIdentity struct {
	UID           string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Provider      string
}

// FirebaseVerifier validates Firebase ID tokens against Google's rotating
// public certificates. It needs no service-account credentials.
type FirebaseVerifier struct {
	projectID string
	client    *http.Client

	mu        sync.Mutex
	certs     map[string]*rsa.PublicKey
	expiresAt time.Time
}

func NewFirebaseVerifier(projectID string) *FirebaseVerifier {
	return &FirebaseVerifier{
		projectID: strings.TrimSpace(projectID),
		client:    &http.Client{Timeout: 10 * time.Second},
		certs:     make(map[string]*rsa.PublicKey),
	}
}

func (v *FirebaseVerifier) Enabled() bool { return v.projectID != "" }

type firebaseClaims struct {
	jwt.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Firebase      struct {
		SignInProvider string `json:"sign_in_provider"`
	} `json:"firebase"`
}

func (v *FirebaseVerifier) Verify(ctx context.Context, rawToken string) (*FirebaseIdentity, error) {
	if !v.Enabled() {
		return nil, ErrFirebaseDisabled
	}
	if len(rawToken) > 4096 {
		return nil, fmt.Errorf("%w: token is too large", ErrFirebaseToken)
	}

	claims := &firebaseClaims{}
	var keyErr error
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(v.projectID),
		jwt.WithIssuer("https://securetoken.google.com/"+v.projectID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)
	token, err := parser.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing key id")
		}
		key, err := v.publicKey(ctx, kid)
		if err != nil {
			keyErr = err
			return nil, err
		}
		return key, nil
	})
	if keyErr != nil {
		return nil, fmt.Errorf("fetch firebase signing keys: %w", keyErr)
	}
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: %v", ErrFirebaseToken, err)
	}

	uid := strings.TrimSpace(claims.Subject)
	if uid == "" || len(uid) > 128 {
		return nil, fmt.Errorf("%w: missing subject", ErrFirebaseToken)
	}
	email := strings.TrimSpace(claims.Email)
	if email == "" {
		return nil, fmt.Errorf("%w: token has no email claim", ErrFirebaseToken)
	}
	if !claims.EmailVerified {
		return nil, ErrEmailUnverified
	}

	return &FirebaseIdentity{
		UID:           uid,
		Email:         email,
		EmailVerified: claims.EmailVerified,
		Name:          strings.TrimSpace(claims.Name),
		Picture:       strings.TrimSpace(claims.Picture),
		Provider:      claims.Firebase.SignInProvider,
	}, nil
}

func (v *FirebaseVerifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if key, ok := v.certs[kid]; ok && time.Now().Before(v.expiresAt) {
		return key, nil
	}

	refreshErr := v.refreshLocked(ctx)
	if key, ok := v.certs[kid]; ok {
		if refreshErr != nil {
			// Serve a stale certificate briefly instead of failing closed.
			v.expiresAt = time.Now().Add(time.Minute)
		}
		return key, nil
	}
	if refreshErr != nil {
		return nil, refreshErr
	}

	// The kid is unknown but the cache was fresh: certificates rotate, so
	// force a single refetch.
	if err := v.refreshLocked(ctx); err != nil {
		return nil, err
	}
	if key, ok := v.certs[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("firebase signing certificate %q not found", kid)
}

func (v *FirebaseVerifier) refreshLocked(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, firebaseCertsURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("firebase certificate endpoint returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	var raw map[string]string
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("decode firebase certificates: %w", err)
	}

	certs := make(map[string]*rsa.PublicKey, len(raw))
	for kid, certPEM := range raw {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		key, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			continue
		}
		certs[kid] = key
	}
	if len(certs) == 0 {
		return errors.New("firebase certificate response contained no usable keys")
	}

	v.certs = certs
	v.expiresAt = time.Now().Add(cacheMaxAge(resp.Header.Get("Cache-Control")))
	return nil
}

func cacheMaxAge(header string) time.Duration {
	const fallback = time.Hour
	for _, directive := range strings.Split(header, ",") {
		directive = strings.TrimSpace(directive)
		value, found := strings.CutPrefix(directive, "max-age=")
		if !found {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || seconds <= 0 {
			continue
		}
		age := time.Duration(seconds) * time.Second
		if age < time.Minute {
			return time.Minute
		}
		if age > 12*time.Hour {
			return 12 * time.Hour
		}
		return age
	}
	return fallback
}
