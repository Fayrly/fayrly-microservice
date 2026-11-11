package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/fayrly/backend/internal/platform/database"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

type userModel struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	Email      string
	Name       string
	Phone      string
	AvatarURL  string `gorm:"column:avatar_url"`
	Status     string
	LastSeenAt *time.Time `gorm:"column:last_seen_at"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time `gorm:"column:deleted_at"`
}

func (userModel) TableName() string { return "users" }

func (m userModel) toDomain() *User {
	return &User{
		ID:         m.ID.String(),
		Email:      m.Email,
		Name:       m.Name,
		Phone:      m.Phone,
		AvatarURL:  m.AvatarURL,
		Status:     m.Status,
		LastSeenAt: m.LastSeenAt,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

type roleModel struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	Key         string
	Name        string
	Description string
	IsSystem    bool `gorm:"column:is_system"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (roleModel) TableName() string { return "roles" }

func (r *Repository) GetUser(ctx context.Context, id string) (*User, error) {
	var m userModel
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	u := m.toDomain()
	if err := r.loadRoles(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var m userModel
	err := r.db.WithContext(ctx).
		Where("lower(email) = lower(?) AND deleted_at IS NULL", email).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	u := m.toDomain()
	if err := r.loadRoles(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// IsDeleted reports whether an id belongs to a soft-deleted user.
func (r *Repository) IsDeleted(ctx context.Context, id string) (bool, error) {
	var deleted bool
	err := r.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM users WHERE id = ? AND deleted_at IS NOT NULL)`, id).
		Scan(&deleted).Error
	return deleted, err
}

type rolePermissionRow struct {
	RoleKey       string
	PermissionKey *string
}

func (r *Repository) loadRoles(ctx context.Context, u *User) error {
	var rows []rolePermissionRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT r.key AS role_key, p.key AS permission_key
		 FROM user_roles ur
		 JOIN roles r ON r.id = ur.role_id
		 LEFT JOIN role_permissions rp ON rp.role_id = r.id
		 LEFT JOIN permissions p ON p.id = rp.permission_id
		 WHERE ur.user_id = ?
		 ORDER BY r.key, p.key`, u.ID).Scan(&rows).Error
	if err != nil {
		return err
	}

	u.Roles = []string{}
	u.Permissions = []string{}
	seenRoles := make(map[string]bool)
	seenPermissions := make(map[string]bool)
	for _, row := range rows {
		if !seenRoles[row.RoleKey] {
			seenRoles[row.RoleKey] = true
			u.Roles = append(u.Roles, row.RoleKey)
		}
		if row.PermissionKey != nil && !seenPermissions[*row.PermissionKey] {
			seenPermissions[*row.PermissionKey] = true
			u.Permissions = append(u.Permissions, *row.PermissionKey)
		}
	}
	return nil
}

func (r *Repository) ListUsers(ctx context.Context, filter UserFilter, limit, offset int) ([]*User, int, error) {
	var args []any
	where := []string{"u.deleted_at IS NULL"}

	if filter.Query != "" {
		pattern := "%" + filter.Query + "%"
		where = append(where, "(u.email ILIKE ? OR u.name ILIKE ?)")
		args = append(args, pattern, pattern)
	}
	if filter.Status != "" {
		where = append(where, "u.status = ?")
		args = append(args, filter.Status)
	}
	if filter.Role != "" {
		where = append(where,
			"EXISTS (SELECT 1 FROM user_roles ur2 JOIN roles r2 ON r2.id = ur2.role_id WHERE ur2.user_id = u.id AND r2.key = ?)")
		args = append(args, filter.Role)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := r.db.WithContext(ctx).Table("users AS u").Where(whereSQL, args...).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var models []userModel
	err := r.db.WithContext(ctx).Table("users AS u").
		Select("u.id, u.email, u.name, u.phone, u.avatar_url, u.status, u.last_seen_at, u.created_at, u.updated_at").
		Where(whereSQL, args...).
		Order("u.created_at DESC, u.id").
		Limit(limit).Offset(offset).
		Scan(&models).Error
	if err != nil {
		return nil, 0, err
	}

	users := make([]*User, 0, len(models))
	for _, m := range models {
		users = append(users, m.toDomain())
	}
	if err := r.loadRolesBulk(ctx, users); err != nil {
		return nil, 0, err
	}
	return users, int(total), nil
}

type userRoleKeyRow struct {
	UserID  string
	RoleKey string
}

func (r *Repository) loadRolesBulk(ctx context.Context, users []*User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]string, 0, len(users))
	byID := make(map[string]*User, len(users))
	for _, u := range users {
		u.Roles = []string{}
		ids = append(ids, u.ID)
		byID[u.ID] = u
	}

	var rows []userRoleKeyRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT ur.user_id, r.key AS role_key
		 FROM user_roles ur
		 JOIN roles r ON r.id = ur.role_id
		 WHERE ur.user_id IN ?
		 ORDER BY r.key`, ids).Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		if u, ok := byID[row.UserID]; ok {
			u.Roles = append(u.Roles, row.RoleKey)
		}
	}
	return nil
}

func (r *Repository) CreateUser(ctx context.Context, u *User, roleKeys []string, assignedBy string) error {
	id, err := uuid.Parse(u.ID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}
	return database.WithTx(ctx, r.db, func(tx *gorm.DB) error {
		model := userModel{
			ID:        id,
			Email:     u.Email,
			Name:      u.Name,
			Phone:     u.Phone,
			AvatarURL: u.AvatarURL,
			Status:    u.Status,
		}
		if err := tx.WithContext(ctx).Create(&model).Error; err != nil {
			if database.IsUniqueViolation(err) {
				return ErrEmailTaken
			}
			return err
		}
		return assignRoles(ctx, tx, u.ID, roleKeys, assignedBy)
	})
}

func assignRoles(ctx context.Context, tx *gorm.DB, userID string, roleKeys []string, assignedBy string) error {
	for _, key := range roleKeys {
		result := tx.WithContext(ctx).Exec(
			`INSERT INTO user_roles (user_id, role_id, assigned_by)
			 SELECT ?, id, NULLIF(?, '')::uuid FROM roles WHERE key = ?`,
			userID, assignedBy, key)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRoleNotFound
		}
	}
	return nil
}

func (r *Repository) UpdateUser(ctx context.Context, id string, patch UserPatch) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE users SET
			name       = COALESCE(?, name),
			phone      = COALESCE(?, phone),
			avatar_url = COALESCE(?, avatar_url),
			status     = COALESCE(?, status),
			updated_at = now()
		 WHERE id = ? AND deleted_at IS NULL`,
		patch.Name, patch.Phone, patch.AvatarURL, patch.Status, id).Error
}

func (r *Repository) SetUserRoles(ctx context.Context, userID string, roleKeys []string, assignedBy string) error {
	return database.WithTx(ctx, r.db, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Exec(`DELETE FROM user_roles WHERE user_id = ?`, userID).Error; err != nil {
			return err
		}
		return assignRoles(ctx, tx, userID, roleKeys, assignedBy)
	})
}

func (r *Repository) AddUserRole(ctx context.Context, userID, roleKey string) error {
	result := r.db.WithContext(ctx).Exec(
		`INSERT INTO user_roles (user_id, role_id)
		 SELECT ?, id FROM roles WHERE key = ?
		 ON CONFLICT (user_id, role_id) DO NOTHING`, userID, roleKey)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func (r *Repository) SoftDeleteUser(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(
		`UPDATE users SET deleted_at = now(), status = 'inactive', updated_at = now()
		 WHERE id = ? AND deleted_at IS NULL`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// HardDeleteUser is only used to compensate a failed cross-service provisioning.
func (r *Repository) HardDeleteUser(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Exec(`DELETE FROM users WHERE id = ?`, id).Error
}

func (r *Repository) CountActiveSuperadmins(ctx context.Context, excludeID string) (int64, error) {
	query := `SELECT count(DISTINCT u.id)
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r ON r.id = ur.role_id
		WHERE r.key = 'superadmin' AND u.status = 'active' AND u.deleted_at IS NULL`
	var args []any
	if excludeID != "" {
		query += " AND u.id <> ?"
		args = append(args, excludeID)
	}
	var count int64
	err := r.db.WithContext(ctx).Raw(query, args...).Scan(&count).Error
	return count, err
}

// --- Roles ------------------------------------------------------------------

type roleRow struct {
	ID          string
	Key         string
	Name        string
	Description string
	IsSystem    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	UserCount   int64
}

func (m roleRow) toDomain() *Role {
	return &Role{
		ID:          m.ID,
		Key:         m.Key,
		Name:        m.Name,
		Description: m.Description,
		IsSystem:    m.IsSystem,
		UserCount:   m.UserCount,
		Permissions: []string{},
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

const roleSelect = `SELECT r.id, r.key, r.name, r.description, r.is_system, r.created_at, r.updated_at,
	(SELECT count(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id AND u.deleted_at IS NULL WHERE ur.role_id = r.id) AS user_count
	FROM roles r`

func (r *Repository) ListRoles(ctx context.Context) ([]*Role, error) {
	var rows []roleRow
	err := r.db.WithContext(ctx).Raw(roleSelect + " ORDER BY r.is_system DESC, r.key").Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	roles := make([]*Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.toDomain())
	}

	permissions, err := r.allRolePermissions(ctx)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		if keys, ok := permissions[role.ID]; ok {
			role.Permissions = keys
		}
	}
	return roles, nil
}

func (r *Repository) allRolePermissions(ctx context.Context) (map[string][]string, error) {
	var rows []struct {
		RoleID string
		Key    string
	}
	err := r.db.WithContext(ctx).Raw(
		`SELECT rp.role_id, p.key
		 FROM role_permissions rp
		 JOIN permissions p ON p.id = rp.permission_id
		 ORDER BY p.key`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string][]string)
	for _, row := range rows {
		result[row.RoleID] = append(result[row.RoleID], row.Key)
	}
	return result, nil
}

func (r *Repository) GetRole(ctx context.Context, id string) (*Role, error) {
	role := &Role{Permissions: []string{}}
	err := r.db.WithContext(ctx).Raw(roleSelect+" WHERE r.id = ?", id).Scan(role).Error
	if err != nil {
		return nil, err
	}
	if role.ID == "" {
		return nil, ErrRoleNotFound
	}

	if err := r.db.WithContext(ctx).Raw(
		`SELECT p.key FROM role_permissions rp
		 JOIN permissions p ON p.id = rp.permission_id
		 WHERE rp.role_id = ? ORDER BY p.key`, id).Scan(&role.Permissions).Error; err != nil {
		return nil, err
	}
	return role, nil
}

func (r *Repository) RoleIDsByKey(ctx context.Context) (map[string]string, error) {
	var rows []struct {
		Key string
		ID  string
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT key, id FROM roles`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.Key] = row.ID
	}
	return result, nil
}

func (r *Repository) PermissionKeys(ctx context.Context) (map[string]bool, error) {
	var rows []struct {
		Key string
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT key FROM permissions`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(rows))
	for _, row := range rows {
		result[row.Key] = true
	}
	return result, nil
}

func (r *Repository) ListPermissions(ctx context.Context) ([]Permission, error) {
	permissions := make([]Permission, 0, 16)
	err := r.db.WithContext(ctx).Raw(
		`SELECT key, resource, action, description FROM permissions ORDER BY resource, action`).
		Scan(&permissions).Error
	return permissions, err
}

func (r *Repository) CreateRole(ctx context.Context, role *Role, permissions []string) error {
	id, err := uuid.Parse(role.ID)
	if err != nil {
		return fmt.Errorf("parse role id: %w", err)
	}
	return database.WithTx(ctx, r.db, func(tx *gorm.DB) error {
		model := roleModel{
			ID:          id,
			Key:         role.Key,
			Name:        role.Name,
			Description: role.Description,
			IsSystem:    false,
		}
		if err := tx.WithContext(ctx).Create(&model).Error; err != nil {
			if database.IsUniqueViolation(err) {
				return ErrRoleExists
			}
			return err
		}
		return replaceRolePermissions(ctx, tx, role.ID, permissions)
	})
}

func (r *Repository) UpdateRole(ctx context.Context, id string, patch UpdateRoleRequest) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE roles SET
			name        = COALESCE(?, name),
			description = COALESCE(?, description),
			updated_at  = now()
		 WHERE id = ?`, patch.Name, patch.Description, id).Error
}

func (r *Repository) DeleteRole(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM roles WHERE id = ? AND is_system = false`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func (r *Repository) CountRoleUsers(ctx context.Context, id string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id AND u.deleted_at IS NULL
		 WHERE ur.role_id = ?`, id).Scan(&count).Error
	return count, err
}

func (r *Repository) SetRolePermissions(ctx context.Context, id string, permissions []string) error {
	return database.WithTx(ctx, r.db, func(tx *gorm.DB) error {
		return replaceRolePermissions(ctx, tx, id, permissions)
	})
}

func (r *Repository) SetAccessMatrix(ctx context.Context, updates []RolePermissionsUpdate) error {
	return database.WithTx(ctx, r.db, func(tx *gorm.DB) error {
		for _, update := range updates {
			if err := replaceRolePermissions(ctx, tx, update.RoleID, update.Permissions); err != nil {
				return err
			}
		}
		return nil
	})
}

func replaceRolePermissions(ctx context.Context, tx *gorm.DB, roleID string, permissions []string) error {
	if err := tx.WithContext(ctx).Exec(`DELETE FROM role_permissions WHERE role_id = ?`, roleID).Error; err != nil {
		return err
	}
	if len(permissions) == 0 {
		return tx.WithContext(ctx).Exec(`UPDATE roles SET updated_at = now() WHERE id = ?`, roleID).Error
	}
	result := tx.WithContext(ctx).Exec(
		`INSERT INTO role_permissions (role_id, permission_id)
		 SELECT ?, id FROM permissions WHERE key IN ?`, roleID, permissions)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != int64(len(permissions)) {
		return ErrPermissionNotFound
	}
	return tx.WithContext(ctx).Exec(`UPDATE roles SET updated_at = now() WHERE id = ?`, roleID).Error
}
