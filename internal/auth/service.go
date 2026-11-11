package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fayrly/backend/internal/auth/userclient"
	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/password"
)

// UserService is implemented by userclient.Client.
type UserService interface {
	EnsureUser(ctx context.Context, input userclient.EnsureInput) (*userclient.Profile, error)
	GetUser(ctx context.Context, id string) (*userclient.Profile, error)
	GetUserByEmail(ctx context.Context, email string) (*userclient.Profile, error)
}

type Service struct {
	repo       *Repository
	users      UserService
	tokens     *authn.Manager
	firebase   *FirebaseVerifier
	mailer     Mailer
	refreshTTL time.Duration
	logger     *slog.Logger
}

func NewService(repo *Repository, users UserService, tokens *authn.Manager, firebase *FirebaseVerifier, mailer Mailer, refreshTTL time.Duration, logger *slog.Logger) *Service {
	if mailer == nil {
		mailer = disabledMailer{}
	}
	return &Service{
		repo:       repo,
		users:      users,
		tokens:     tokens,
		firebase:   firebase,
		mailer:     mailer,
		refreshTTL: refreshTTL,
		logger:     logger,
	}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest, meta RequestMeta) (*Session, error) {
	email := password.NormalizeEmail(req.Email)
	if err := password.Validate(req.Password); err != nil {
		return nil, apierr.Validation(map[string]string{"password": err.Error()})
	}
	hash, err := password.Hash(req.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	id := uuid.NewString()
	credential := &Credential{
		ID:           id,
		Email:        email,
		PasswordHash: &hash,
		Provider:     ProviderPassword,
	}
	if err := s.repo.CreateCredential(ctx, credential); err != nil {
		return nil, mapAuthError(err)
	}

	profile, err := s.users.EnsureUser(ctx, userclient.EnsureInput{
		ID:    id,
		Email: email,
		Name:  strings.TrimSpace(req.Name),
		Phone: strings.TrimSpace(req.Phone),
		Role:  "member",
	})
	if err != nil {
		if cleanupErr := s.deleteCredential(ctx, id); cleanupErr != nil {
			s.logger.Error("failed to roll back credential after profile provisioning failure",
				"user_id", id, "error", cleanupErr)
		}
		return nil, err
	}
	if err := checkActive(profile); err != nil {
		return nil, err
	}
	return s.issueSession(ctx, profile, meta, "")
}

func (s *Service) Login(ctx context.Context, req LoginRequest, meta RequestMeta) (*Session, error) {
	credential, err := s.repo.GetCredentialByEmail(ctx, password.NormalizeEmail(req.Email))
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			password.VerifyDummy(req.Password)
			return nil, mapAuthError(ErrInvalidCredentials)
		}
		return nil, err
	}
	if credential.PasswordHash == nil || !password.Verify(*credential.PasswordHash, req.Password) {
		return nil, mapAuthError(ErrInvalidCredentials)
	}

	profile, err := s.users.GetUser(ctx, credential.ID)
	if err != nil {
		return nil, err
	}
	if err := checkActive(profile); err != nil {
		return nil, err
	}
	return s.issueSession(ctx, profile, meta, "")
}

func (s *Service) FirebaseLogin(ctx context.Context, req FirebaseRequest, meta RequestMeta) (*Session, error) {
	if !s.firebase.Enabled() {
		return nil, mapAuthError(ErrFirebaseDisabled)
	}
	identity, err := s.firebase.Verify(ctx, req.IDToken)
	if err != nil {
		return nil, mapAuthError(err)
	}
	email := password.NormalizeEmail(identity.Email)

	credential, err := s.repo.GetCredentialByFirebaseUID(ctx, identity.UID)
	if err == nil {
		return s.finishFirebaseLogin(ctx, credential, meta)
	}
	if !errors.Is(err, ErrCredentialNotFound) {
		return nil, err
	}

	credential, err = s.repo.GetCredentialByEmail(ctx, email)
	switch {
	case err == nil:
		// Link the Firebase identity to an existing local account, but never
		// steal an account already linked to a different Firebase user.
		if credential.FirebaseUID != nil && *credential.FirebaseUID != identity.UID {
			return nil, mapAuthError(ErrFirebaseUIDTaken)
		}
		if credential.FirebaseUID == nil {
			if err := s.repo.LinkFirebase(ctx, credential.ID, identity.UID); err != nil {
				return nil, mapAuthError(err)
			}
		}
		return s.finishFirebaseLogin(ctx, credential, meta)
	case !errors.Is(err, ErrCredentialNotFound):
		return nil, err
	}

	name := identity.Name
	if name == "" {
		name = localPart(email)
	}
	id := uuid.NewString()
	credential = &Credential{
		ID:          id,
		Email:       email,
		FirebaseUID: &identity.UID,
		Provider:    ProviderFirebase,
	}
	if err := s.repo.CreateCredential(ctx, credential); err != nil {
		return nil, mapAuthError(err)
	}

	profile, err := s.users.EnsureUser(ctx, userclient.EnsureInput{
		ID:    id,
		Email: email,
		Name:  name,
		Role:  "member",
	})
	if err != nil {
		if cleanupErr := s.deleteCredential(ctx, id); cleanupErr != nil {
			s.logger.Error("failed to roll back firebase credential after provisioning failure",
				"user_id", id, "error", cleanupErr)
		}
		return nil, err
	}
	if err := checkActive(profile); err != nil {
		return nil, err
	}
	return s.issueSession(ctx, profile, meta, "")
}

func (s *Service) finishFirebaseLogin(ctx context.Context, credential *Credential, meta RequestMeta) (*Session, error) {
	profile, err := s.users.GetUser(ctx, credential.ID)
	if err != nil {
		return nil, err
	}
	if err := checkActive(profile); err != nil {
		return nil, err
	}
	return s.issueSession(ctx, profile, meta, "")
}

func (s *Service) Refresh(ctx context.Context, rawToken string, meta RequestMeta) (*Session, error) {
	hash := hashRefreshToken(rawToken)
	token, err := s.repo.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return nil, mapAuthError(ErrRefreshInvalid)
		}
		return nil, err
	}
	if token.RevokedAt != nil {
		// A replayed token means the family is compromised: kill every session
		// in it.
		if err := s.repo.RevokeFamily(ctx, token.FamilyID); err != nil {
			s.logger.Error("failed to revoke refresh token family after reuse", "family_id", token.FamilyID, "error", err)
		}
		return nil, mapAuthError(ErrRefreshReused)
	}
	if time.Now().After(token.ExpiresAt) {
		return nil, mapAuthError(ErrRefreshInvalid)
	}

	profile, err := s.users.GetUser(ctx, token.UserID)
	if err != nil {
		return nil, err
	}
	if err := checkActive(profile); err != nil {
		if revokeErr := s.repo.RevokeFamily(ctx, token.FamilyID); revokeErr != nil {
			s.logger.Error("failed to revoke refresh token family for inactive account", "family_id", token.FamilyID, "error", revokeErr)
		}
		return nil, err
	}

	replacementRaw, replacement, err := s.newRefreshToken(profile.ID, token.FamilyID, meta)
	if err != nil {
		return nil, err
	}
	rotated, err := s.repo.RotateRefreshToken(ctx, token.ID, replacement)
	if err != nil {
		return nil, err
	}
	if !rotated {
		if revokeErr := s.repo.RevokeFamily(ctx, token.FamilyID); revokeErr != nil {
			s.logger.Error("failed to revoke refresh token family after rotation race", "family_id", token.FamilyID, "error", revokeErr)
		}
		return nil, mapAuthError(ErrRefreshReused)
	}

	accessToken, err := s.issueAccessToken(profile, token.FamilyID)
	if err != nil {
		return nil, err
	}
	return &Session{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(s.tokens.AccessTTL().Seconds()),
		RefreshToken:     replacementRaw,
		RefreshExpiresIn: int64(s.refreshTTL.Seconds()),
		User:             sessionUser(profile),
	}, nil
}

func (s *Service) Logout(ctx context.Context, rawToken string) error {
	hash := hashRefreshToken(rawToken)
	token, err := s.repo.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return nil
		}
		return err
	}
	return s.repo.RevokeFamily(ctx, token.FamilyID)
}

func (s *Service) ChangePassword(ctx context.Context, userID string, req ChangePasswordRequest) error {
	credential, err := s.repo.GetCredentialByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return apierr.Unauthorized("")
		}
		return err
	}
	if credential.PasswordHash == nil {
		return mapAuthError(ErrNoPassword)
	}
	if !password.Verify(*credential.PasswordHash, req.CurrentPassword) {
		return apierr.BadRequest("current password is incorrect")
	}
	if err := password.Validate(req.NewPassword); err != nil {
		return apierr.Validation(map[string]string{"new_password": err.Error()})
	}
	if req.CurrentPassword == req.NewPassword {
		return apierr.Validation(map[string]string{"new_password": "must be different from the current password"})
	}
	hash, err := password.Hash(req.NewPassword)
	if err != nil {
		return err
	}
	if err := s.repo.SetPassword(ctx, userID, hash); err != nil {
		return err
	}
	if err := s.repo.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	return nil
}

// RequestPasswordReset emails a short-lived code to the account's address. The
// response intentionally does not reveal whether the email is registered.
func (s *Service) RequestPasswordReset(ctx context.Context, req ForgotPasswordRequest) error {
	if !s.mailer.Enabled() {
		return mapAuthError(ErrMailUnavailable)
	}
	email := password.NormalizeEmail(req.Email)
	credential, err := s.repo.GetCredentialByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return nil
		}
		return err
	}

	latest, err := s.repo.LatestPasswordResetCode(ctx, credential.ID)
	if err != nil {
		return err
	}
	if latest != nil && time.Since(latest.CreatedAt) < resetCodeCooldown {
		return mapAuthError(ErrResetCooldown)
	}

	code, salt, hash, err := generateResetCode()
	if err != nil {
		return err
	}
	record := &PasswordResetCode{
		ID:           uuid.NewString(),
		CredentialID: credential.ID,
		CodeHash:     hash,
		Salt:         salt,
		ExpiresAt:    time.Now().Add(resetCodeTTL),
	}
	if err := s.repo.CreatePasswordResetCode(ctx, record); err != nil {
		return err
	}
	if err := s.mailer.Send(ctx, passwordResetMessage(credential.Email, code, resetCodeTTL)); err != nil {
		s.logger.Error("failed to send password reset email", "user_id", credential.ID, "error", err)
		if cleanupErr := s.repo.DeletePasswordResetCode(ctx, record.ID); cleanupErr != nil {
			s.logger.Error("failed to discard password reset code after email failure",
				"user_id", credential.ID, "error", cleanupErr)
		}
		return mapAuthError(ErrMailDelivery)
	}
	return nil
}

// VerifyResetCode checks a reset code without consuming it so a multi-step
// client can validate the OTP before asking for the new password.
func (s *Service) VerifyResetCode(ctx context.Context, req VerifyResetCodeRequest) error {
	_, _, err := s.checkResetCode(ctx, req.Email, req.Code)
	return err
}

// ResetPasswordWithCode verifies a reset code and replaces the account
// password. Every existing session is revoked on success.
func (s *Service) ResetPasswordWithCode(ctx context.Context, req ResetPasswordRequest) error {
	if err := password.Validate(req.Password); err != nil {
		return apierr.Validation(map[string]string{"password": err.Error()})
	}

	credential, record, err := s.checkResetCode(ctx, req.Email, req.Code)
	if err != nil {
		return err
	}

	hash, err := password.Hash(req.Password)
	if err != nil {
		return err
	}
	consumed, err := s.repo.ConsumePasswordResetCode(ctx, record.ID)
	if err != nil {
		return err
	}
	if !consumed {
		return mapAuthError(ErrResetCodeInvalid)
	}
	if err := s.repo.SetPassword(ctx, credential.ID, hash); err != nil {
		return err
	}
	if err := s.repo.RevokeAllForUser(ctx, credential.ID); err != nil {
		return err
	}
	if s.mailer.Enabled() {
		if err := s.mailer.Send(ctx, passwordChangedMessage(credential.Email, time.Now())); err != nil {
			s.logger.Error("failed to send password changed notification", "user_id", credential.ID, "error", err)
		}
	}
	return nil
}

func (s *Service) checkResetCode(ctx context.Context, email, code string) (*Credential, *PasswordResetCode, error) {
	credential, err := s.repo.GetCredentialByEmail(ctx, password.NormalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return nil, nil, mapAuthError(ErrResetCodeInvalid)
		}
		return nil, nil, err
	}

	record, err := s.repo.LatestPasswordResetCode(ctx, credential.ID)
	if err != nil {
		return nil, nil, err
	}
	if record == nil || time.Now().After(record.ExpiresAt) {
		return nil, nil, mapAuthError(ErrResetCodeInvalid)
	}
	if record.Attempts >= resetCodeMaxAttempts {
		return nil, nil, mapAuthError(ErrResetCodeAttempts)
	}
	if !verifyResetCode(record, code) {
		attempts, err := s.repo.IncrementPasswordResetAttempts(ctx, record.ID)
		if err != nil {
			return nil, nil, err
		}
		if attempts >= resetCodeMaxAttempts {
			return nil, nil, mapAuthError(ErrResetCodeAttempts)
		}
		return nil, nil, mapAuthError(ErrResetCodeInvalid)
	}
	return credential, record, nil
}

func (s *Service) Me(ctx context.Context, userID string) (*userclient.Profile, error) {
	profile, err := s.users.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// BootstrapAdmin ensures the configured operator account exists with the
// superadmin role. It is safe to call on every boot.
func (s *Service) BootstrapAdmin(ctx context.Context, email, plainPassword, name string) error {
	email = password.NormalizeEmail(email)
	if err := password.Validate(plainPassword); err != nil {
		return fmt.Errorf("bootstrap password does not meet the policy: %w", err)
	}

	if _, err := s.repo.GetCredentialByEmail(ctx, email); err == nil {
		return nil
	} else if !errors.Is(err, ErrCredentialNotFound) {
		return err
	}

	adminName := strings.TrimSpace(name)
	if adminName == "" {
		adminName = "Platform Owner"
	}
	userID := ""
	if profile, err := s.users.GetUserByEmail(ctx, email); err == nil {
		userID = profile.ID
		if profile.Name != "" {
			adminName = profile.Name
		}
	}
	if userID == "" {
		userID = uuid.NewString()
	}

	if _, err := s.users.EnsureUser(ctx, userclient.EnsureInput{
		ID:    userID,
		Email: email,
		Name:  adminName,
		Role:  "superadmin",
	}); err != nil {
		return err
	}

	hash, err := password.Hash(plainPassword)
	if err != nil {
		return err
	}
	return mapAuthError(s.repo.CreateCredential(ctx, &Credential{
		ID:           userID,
		Email:        email,
		PasswordHash: &hash,
		Provider:     ProviderPassword,
	}))
}

// --- internal credential API ---------------------------------------------

func (s *Service) CreateCredential(ctx context.Context, req CreateCredentialRequest) (*CredentialResponse, error) {
	email := password.NormalizeEmail(req.Email)
	plain := req.Password
	temporary := ""
	if plain == "" {
		generated, err := password.Generate()
		if err != nil {
			return nil, err
		}
		plain = generated
		temporary = generated
	} else if err := password.Validate(plain); err != nil {
		return nil, apierr.Validation(map[string]string{"password": err.Error()})
	}

	hash, err := password.Hash(plain)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateCredential(ctx, &Credential{
		ID:           req.UserID,
		Email:        email,
		PasswordHash: &hash,
		Provider:     ProviderPassword,
	}); err != nil {
		return nil, mapAuthError(err)
	}
	return &CredentialResponse{TemporaryPassword: temporary}, nil
}

func (s *Service) DeleteCredential(ctx context.Context, userID string) error {
	deleted, err := s.repo.DeleteCredential(ctx, userID)
	if err != nil {
		return err
	}
	if !deleted {
		return apierr.NotFound("credential")
	}
	return nil
}

func (s *Service) ResetPassword(ctx context.Context, userID string) (*CredentialResponse, error) {
	if _, err := s.repo.GetCredentialByID(ctx, userID); err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return nil, apierr.NotFound("credential")
		}
		return nil, err
	}
	plain, err := password.Generate()
	if err != nil {
		return nil, err
	}
	hash, err := password.Hash(plain)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetPassword(ctx, userID, hash); err != nil {
		return nil, err
	}
	if err := s.repo.RevokeAllForUser(ctx, userID); err != nil {
		return nil, err
	}
	return &CredentialResponse{TemporaryPassword: plain}, nil
}

func (s *Service) CleanupExpiredTokens(ctx context.Context) (int64, error) {
	tokens, err := s.repo.DeleteExpiredRefreshTokens(ctx)
	if err != nil {
		return tokens, err
	}
	codes, err := s.repo.DeleteExpiredPasswordResetCodes(ctx)
	return tokens + codes, err
}

// --- helpers --------------------------------------------------------------

func (s *Service) issueSession(ctx context.Context, profile *userclient.Profile, meta RequestMeta, familyID string) (*Session, error) {
	rawToken, token, err := s.newRefreshToken(profile.ID, familyID, meta)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateRefreshToken(ctx, token); err != nil {
		return nil, err
	}
	accessToken, err := s.issueAccessToken(profile, token.FamilyID)
	if err != nil {
		return nil, err
	}
	return &Session{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(s.tokens.AccessTTL().Seconds()),
		RefreshToken:     rawToken,
		RefreshExpiresIn: int64(s.refreshTTL.Seconds()),
		User:             sessionUser(profile),
	}, nil
}

func (s *Service) issueAccessToken(profile *userclient.Profile, familyID string) (string, error) {
	accessToken, _, err := s.tokens.Issue(authn.Identity{
		UserID:      profile.ID,
		Email:       profile.Email,
		Name:        profile.Name,
		Roles:       profile.Roles,
		Permissions: profile.Permissions,
		SessionID:   familyID,
	})
	return accessToken, err
}

func (s *Service) newRefreshToken(userID, familyID string, meta RequestMeta) (string, *RefreshToken, error) {
	if familyID == "" {
		familyID = uuid.NewString()
	}
	rawToken, hash, err := generateRefreshToken()
	if err != nil {
		return "", nil, err
	}
	userAgent := meta.UserAgent
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	return rawToken, &RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: hash,
		UserAgent: userAgent,
		IP:        meta.IP,
		ExpiresAt: time.Now().Add(s.refreshTTL),
	}, nil
}

func (s *Service) deleteCredential(ctx context.Context, id string) error {
	_, err := s.repo.DeleteCredential(ctx, id)
	return err
}

func generateRefreshToken() (string, []byte, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buffer)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

func hashRefreshToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

const (
	resetCodeTTL         = 15 * time.Minute
	resetCodeCooldown    = 60 * time.Second
	resetCodeMaxAttempts = 5
)

func generateResetCode() (string, []byte, []byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", nil, nil, fmt.Errorf("generate reset code salt: %w", err)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", nil, nil, fmt.Errorf("generate reset code: %w", err)
	}
	code := fmt.Sprintf("%06d", n.Int64())
	return code, salt, hashResetCode(salt, code), nil
}

func hashResetCode(salt []byte, code string) []byte {
	digest := sha256.New()
	digest.Write(salt)
	digest.Write([]byte(code))
	return digest.Sum(nil)
}

func verifyResetCode(record *PasswordResetCode, code string) bool {
	return subtle.ConstantTimeCompare(record.CodeHash, hashResetCode(record.Salt, code)) == 1
}

func checkActive(profile *userclient.Profile) error {
	if profile.Status != StatusActive {
		return mapAuthError(ErrAccountInactive)
	}
	return nil
}

func sessionUser(profile *userclient.Profile) SessionUser {
	roles := profile.Roles
	if roles == nil {
		roles = []string{}
	}
	permissions := profile.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return SessionUser{
		ID:          profile.ID,
		Email:       profile.Email,
		Name:        profile.Name,
		Status:      profile.Status,
		Roles:       roles,
		Permissions: permissions,
	}
}

func localPart(email string) string {
	name, _, _ := strings.Cut(email, "@")
	if name == "" {
		return "Fayrly Member"
	}
	return name
}

func mapAuthError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	switch {
	case errors.Is(err, ErrCredentialNotFound):
		return apierr.NotFound("credential")
	case errors.Is(err, ErrEmailTaken):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrFirebaseUIDTaken):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrCredentialExists):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrInvalidCredentials):
		return apierr.Unauthorized("invalid email or password")
	case errors.Is(err, ErrNoPassword):
		return apierr.Conflict("this account signs in with Firebase and has no password set")
	case errors.Is(err, ErrAccountInactive):
		return apierr.Forbidden("this account is not active; contact an administrator")
	case errors.Is(err, ErrFirebaseDisabled):
		return apierr.ServiceUnavailable("Firebase sign-in is not configured")
	case errors.Is(err, ErrFirebaseToken):
		return apierr.Unauthorized("Firebase ID token is invalid or expired")
	case errors.Is(err, ErrEmailUnverified):
		return apierr.Forbidden("the Firebase account email must be verified before signing in")
	case errors.Is(err, ErrRefreshInvalid):
		return apierr.Unauthorized("refresh token is invalid or expired")
	case errors.Is(err, ErrRefreshReused):
		return apierr.Unauthorized("refresh token has already been used; all sessions in this family were revoked")
	case errors.Is(err, ErrResetCodeInvalid):
		return apierr.BadRequest("reset code is invalid or expired")
	case errors.Is(err, ErrResetCodeAttempts):
		return apierr.TooManyRequests("too many incorrect reset code attempts; request a new code")
	case errors.Is(err, ErrResetCooldown):
		return apierr.TooManyRequests("wait a minute before requesting another reset code")
	case errors.Is(err, ErrMailUnavailable):
		return apierr.ServiceUnavailable("password reset email delivery is not configured")
	case errors.Is(err, ErrMailDelivery):
		return apierr.ServiceUnavailable("the reset email could not be delivered; try again later")
	default:
		return err
	}
}
