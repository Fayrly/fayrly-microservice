package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/fayrly/backend/internal/platform/database"
)

// ErrCredentialExists indicates a credential row already exists for the id.
var ErrCredentialExists = errors.New("credentials already exist for this user")

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

type credentialModel struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	Email        string
	PasswordHash *string `gorm:"column:password_hash"`
	FirebaseUID  *string `gorm:"column:firebase_uid"`
	Provider     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (credentialModel) TableName() string { return "credentials" }

type refreshTokenModel struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID `gorm:"column:user_id;type:uuid"`
	FamilyID   uuid.UUID `gorm:"column:family_id;type:uuid"`
	TokenHash  []byte    `gorm:"column:token_hash"`
	UserAgent  string    `gorm:"column:user_agent"`
	IP         string
	ExpiresAt  time.Time  `gorm:"column:expires_at"`
	RevokedAt  *time.Time `gorm:"column:revoked_at"`
	ReplacedBy *uuid.UUID `gorm:"column:replaced_by;type:uuid"`
	CreatedAt  time.Time
}

func (refreshTokenModel) TableName() string { return "refresh_tokens" }

type passwordResetCodeModel struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	CredentialID uuid.UUID `gorm:"column:credential_id;type:uuid"`
	CodeHash     []byte    `gorm:"column:code_hash"`
	Salt         []byte
	ExpiresAt    time.Time  `gorm:"column:expires_at"`
	ConsumedAt   *time.Time `gorm:"column:consumed_at"`
	Attempts     int
	CreatedAt    time.Time
}

func (passwordResetCodeModel) TableName() string { return "password_reset_codes" }

func (m passwordResetCodeModel) toDomain() *PasswordResetCode {
	return &PasswordResetCode{
		ID:           m.ID.String(),
		CredentialID: m.CredentialID.String(),
		CodeHash:     m.CodeHash,
		Salt:         m.Salt,
		ExpiresAt:    m.ExpiresAt,
		ConsumedAt:   m.ConsumedAt,
		Attempts:     m.Attempts,
		CreatedAt:    m.CreatedAt,
	}
}

func (m credentialModel) toDomain() *Credential {
	return &Credential{
		ID:           m.ID.String(),
		Email:        m.Email,
		PasswordHash: m.PasswordHash,
		FirebaseUID:  m.FirebaseUID,
		Provider:     m.Provider,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func (m refreshTokenModel) toDomain() *RefreshToken {
	token := &RefreshToken{
		ID:        m.ID.String(),
		UserID:    m.UserID.String(),
		FamilyID:  m.FamilyID.String(),
		TokenHash: m.TokenHash,
		UserAgent: m.UserAgent,
		IP:        m.IP,
		ExpiresAt: m.ExpiresAt,
		RevokedAt: m.RevokedAt,
		CreatedAt: m.CreatedAt,
	}
	if m.ReplacedBy != nil {
		replacedBy := m.ReplacedBy.String()
		token.ReplacedBy = &replacedBy
	}
	return token
}

func (r *Repository) CreateCredential(ctx context.Context, c *Credential) error {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return fmt.Errorf("parse credential id: %w", err)
	}
	model := credentialModel{
		ID:           id,
		Email:        c.Email,
		PasswordHash: c.PasswordHash,
		FirebaseUID:  c.FirebaseUID,
		Provider:     c.Provider,
	}
	return mapCredentialUnique(r.db.WithContext(ctx).Create(&model).Error)
}

func (r *Repository) GetCredentialByEmail(ctx context.Context, email string) (*Credential, error) {
	return r.firstCredential(ctx, "lower(email) = lower(?)", email)
}

func (r *Repository) GetCredentialByID(ctx context.Context, id string) (*Credential, error) {
	return r.firstCredential(ctx, "id = ?", id)
}

func (r *Repository) GetCredentialByFirebaseUID(ctx context.Context, uid string) (*Credential, error) {
	return r.firstCredential(ctx, "firebase_uid = ?", uid)
}

func (r *Repository) firstCredential(ctx context.Context, where string, args ...any) (*Credential, error) {
	var model credentialModel
	err := r.db.WithContext(ctx).Where(where, args...).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return model.toDomain(), nil
}

func (r *Repository) LinkFirebase(ctx context.Context, id, firebaseUID string) error {
	err := r.db.WithContext(ctx).Model(&credentialModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"firebase_uid": firebaseUID,
			"provider":     gorm.Expr("CASE WHEN password_hash IS NULL THEN 'firebase' ELSE 'linked' END"),
			"updated_at":   time.Now().UTC(),
		}).Error
	return mapCredentialUnique(err)
}

func (r *Repository) SetPassword(ctx context.Context, id, hash string) error {
	return r.db.WithContext(ctx).Model(&credentialModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"password_hash": hash,
			"provider":      gorm.Expr("CASE WHEN firebase_uid IS NULL THEN 'password' ELSE 'linked' END"),
			"updated_at":    time.Now().UTC(),
		}).Error
}

func (r *Repository) DeleteCredential(ctx context.Context, id string) (bool, error) {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&credentialModel{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *Repository) CreateRefreshToken(ctx context.Context, token *RefreshToken) error {
	model, err := refreshTokenToModel(token)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(model).Error
}

func (r *Repository) GetRefreshTokenByHash(ctx context.Context, hash []byte) (*RefreshToken, error) {
	var model refreshTokenModel
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return model.toDomain(), nil
}

// RotateRefreshToken atomically revokes oldID and stores its replacement. It
// returns false when another request already rotated the token.
func (r *Repository) RotateRefreshToken(ctx context.Context, oldID string, replacement *RefreshToken) (bool, error) {
	rotated := false
	err := database.WithTx(ctx, r.db, func(tx *gorm.DB) error {
		result := tx.Model(&refreshTokenModel{}).
			Where("id = ? AND revoked_at IS NULL", oldID).
			Updates(map[string]any{
				"revoked_at":  time.Now().UTC(),
				"replaced_by": replacement.ID,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		rotated = true

		model, err := refreshTokenToModel(replacement)
		if err != nil {
			return err
		}
		return tx.Create(model).Error
	})
	return rotated, err
}

func (r *Repository) RevokeFamily(ctx context.Context, familyID string) error {
	return r.db.WithContext(ctx).Model(&refreshTokenModel{}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Update("revoked_at", time.Now().UTC()).Error
}

func (r *Repository) RevokeAllForUser(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Model(&refreshTokenModel{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", time.Now().UTC()).Error
}

// DeleteExpiredRefreshTokens removes tokens that expired over a day ago.
func (r *Repository) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("expires_at < now() - interval '1 day'").
		Delete(&refreshTokenModel{})
	return result.RowsAffected, result.Error
}

// CreatePasswordResetCode stores a new code and invalidates any unconsumed code
// still outstanding for the same credential.
func (r *Repository) CreatePasswordResetCode(ctx context.Context, code *PasswordResetCode) error {
	id, err := uuid.Parse(code.ID)
	if err != nil {
		return fmt.Errorf("parse reset code id: %w", err)
	}
	credentialID, err := uuid.Parse(code.CredentialID)
	if err != nil {
		return fmt.Errorf("parse reset code credential id: %w", err)
	}
	model := passwordResetCodeModel{
		ID:           id,
		CredentialID: credentialID,
		CodeHash:     code.CodeHash,
		Salt:         code.Salt,
		ExpiresAt:    code.ExpiresAt,
	}
	return database.WithTx(ctx, r.db, func(tx *gorm.DB) error {
		if err := tx.Model(&passwordResetCodeModel{}).
			Where("credential_id = ? AND consumed_at IS NULL", credentialID).
			Update("consumed_at", time.Now().UTC()).Error; err != nil {
			return err
		}
		return tx.Create(&model).Error
	})
}

// LatestPasswordResetCode returns the newest unconsumed code, or nil when none
// is outstanding.
func (r *Repository) LatestPasswordResetCode(ctx context.Context, credentialID string) (*PasswordResetCode, error) {
	var model passwordResetCodeModel
	err := r.db.WithContext(ctx).
		Where("credential_id = ? AND consumed_at IS NULL", credentialID).
		Order("created_at DESC").
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model.toDomain(), nil
}

// ConsumePasswordResetCode marks a code as used. It returns false when another
// request consumed it first.
func (r *Repository) ConsumePasswordResetCode(ctx context.Context, id string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&passwordResetCodeModel{}).
		Where("id = ? AND consumed_at IS NULL", id).
		Update("consumed_at", time.Now().UTC())
	return result.RowsAffected > 0, result.Error
}

// IncrementPasswordResetAttempts records a failed guess and returns the new
// attempt count.
func (r *Repository) IncrementPasswordResetAttempts(ctx context.Context, id string) (int, error) {
	if err := r.db.WithContext(ctx).Model(&passwordResetCodeModel{}).
		Where("id = ?", id).
		UpdateColumn("attempts", gorm.Expr("attempts + 1")).Error; err != nil {
		return 0, err
	}
	var model passwordResetCodeModel
	if err := r.db.WithContext(ctx).Select("attempts").First(&model, "id = ?", id).Error; err != nil {
		return 0, err
	}
	return model.Attempts, nil
}

func (r *Repository) DeletePasswordResetCode(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&passwordResetCodeModel{}).Error
}

// DeleteExpiredPasswordResetCodes removes codes that expired over a day ago.
func (r *Repository) DeleteExpiredPasswordResetCodes(ctx context.Context) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("expires_at < now() - interval '1 day'").
		Delete(&passwordResetCodeModel{})
	return result.RowsAffected, result.Error
}

func refreshTokenToModel(token *RefreshToken) (*refreshTokenModel, error) {
	id, err := uuid.Parse(token.ID)
	if err != nil {
		return nil, fmt.Errorf("parse refresh token id: %w", err)
	}
	userID, err := uuid.Parse(token.UserID)
	if err != nil {
		return nil, fmt.Errorf("parse refresh token user id: %w", err)
	}
	familyID, err := uuid.Parse(token.FamilyID)
	if err != nil {
		return nil, fmt.Errorf("parse refresh token family id: %w", err)
	}
	return &refreshTokenModel{
		ID:        id,
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: token.TokenHash,
		UserAgent: token.UserAgent,
		IP:        token.IP,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

func mapCredentialUnique(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch {
		case strings.Contains(pgErr.ConstraintName, "email"):
			return ErrEmailTaken
		case strings.Contains(pgErr.ConstraintName, "firebase"):
			return ErrFirebaseUIDTaken
		default:
			return ErrCredentialExists
		}
	}
	return err
}
