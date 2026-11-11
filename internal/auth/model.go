// Package auth implements registration, sign-in (password and Firebase), JWT
// access tokens and rotating opaque refresh tokens.
package auth

import (
	"errors"
	"time"
)

var (
	ErrCredentialNotFound = errors.New("credential not found")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrFirebaseUIDTaken   = errors.New("this Firebase account is already linked to another user")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNoPassword         = errors.New("this account has no password set")
	ErrAccountInactive    = errors.New("account is not active")
	ErrFirebaseDisabled   = errors.New("firebase sign-in is not configured")
	ErrFirebaseToken      = errors.New("invalid firebase id token")
	ErrEmailUnverified    = errors.New("the firebase account email is not verified")
	ErrRefreshInvalid     = errors.New("refresh token is invalid or expired")
	ErrRefreshReused      = errors.New("refresh token has already been used")
	ErrResetCodeInvalid   = errors.New("reset code is invalid or expired")
	ErrResetCodeAttempts  = errors.New("too many incorrect reset code attempts")
	ErrResetCooldown      = errors.New("a reset code was requested moments ago")
	ErrMailUnavailable    = errors.New("password reset email delivery is not configured")
	ErrMailDelivery       = errors.New("password reset email could not be delivered")
)

const (
	ProviderPassword = "password"
	ProviderFirebase = "firebase"
	ProviderLinked   = "linked"

	StatusActive = "active"
)

type Credential struct {
	ID           string
	Email        string
	PasswordHash *string
	FirebaseUID  *string
	Provider     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RefreshToken struct {
	ID         string
	UserID     string
	FamilyID   string
	TokenHash  []byte
	UserAgent  string
	IP         string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	ReplacedBy *string
	CreatedAt  time.Time
}

// PasswordResetCode is a one-time, short-lived code that authorises a password
// reset for a single credential. Only a salted digest of the code is stored.
type PasswordResetCode struct {
	ID           string
	CredentialID string
	CodeHash     []byte
	Salt         []byte
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	Attempts     int
	CreatedAt    time.Time
}

// Profile is the identity data owned by the user service.
type Profile struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

type SessionUser struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

type Session struct {
	AccessToken      string      `json:"access_token"`
	TokenType        string      `json:"token_type"`
	ExpiresIn        int64       `json:"expires_in"`
	RefreshToken     string      `json:"refresh_token"`
	RefreshExpiresIn int64       `json:"refresh_expires_in"`
	User             SessionUser `json:"user"`
}

type RequestMeta struct {
	UserAgent string
	IP        string
}

type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=8,max=72"`
	Name     string `json:"name" validate:"required,min=2,max=120"`
	Phone    string `json:"phone" validate:"omitempty,max=32"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,max=72"`
}

type FirebaseRequest struct {
	IDToken string `json:"id_token" validate:"required,max=4096"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required,max=512"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required,max=512"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required,max=72"`
	NewPassword     string `json:"new_password" validate:"required,min=8,max=72"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

type VerifyResetCodeRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
	Code  string `json:"code" validate:"required,len=6,numeric"`
}

type ResetPasswordRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Code     string `json:"code" validate:"required,len=6,numeric"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

type CreateCredentialRequest struct {
	UserID   string `json:"user_id" validate:"required,uuid"`
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"omitempty,min=8,max=72"`
}

type CredentialResponse struct {
	TemporaryPassword string `json:"temporary_password,omitempty"`
}

type MeResponse struct {
	User Profile `json:"user"`
}
