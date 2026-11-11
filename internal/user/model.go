// Package user implements user administration, roles and the RBAC access matrix.
package user

import (
	"errors"
	"time"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrDeletedUser        = errors.New("user has been deleted")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrRoleNotFound       = errors.New("role not found")
	ErrPermissionNotFound = errors.New("permission not found")
	ErrRoleExists         = errors.New("role key is already in use")
	ErrRoleInUse          = errors.New("role is still assigned to users")
	ErrSystemRole         = errors.New("system roles cannot be deleted")
	ErrSuperadminLocked   = errors.New("the superadmin role is managed by the system")
	ErrLastSuperadmin     = errors.New("at least one active superadmin must remain")
	ErrSelfModification   = errors.New("you cannot perform this action on your own account")
)

const (
	StatusActive    = "active"
	StatusInactive  = "inactive"
	StatusSuspended = "suspended"

	RoleMember     = "member"
	RoleAdmin      = "admin"
	RoleSuperadmin = "superadmin"
)

type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Phone       string     `json:"phone"`
	AvatarURL   string     `json:"avatar_url"`
	Status      string     `json:"status"`
	Roles       []string   `json:"roles"`
	Permissions []string   `json:"permissions,omitempty"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Role struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	UserCount   int64     `json:"user_count"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Permission struct {
	Key         string `json:"key"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

type AccessMatrix struct {
	Permissions []Permission `json:"permissions"`
	Roles       []*Role      `json:"roles"`
}

type UserFilter struct {
	Query  string
	Status string
	Role   string
}

type UserPatch struct {
	Name      *string
	Phone     *string
	AvatarURL *string
	Status    *string
}

type RolePermissionsUpdate struct {
	RoleID      string
	Permissions []string
}

// --- Request payloads -------------------------------------------------------

type CreateUserRequest struct {
	Email     string   `json:"email" validate:"required,email,max=254"`
	Name      string   `json:"name" validate:"required,min=2,max=120"`
	Phone     string   `json:"phone" validate:"omitempty,max=32"`
	AvatarURL string   `json:"avatar_url" validate:"omitempty,url,max=500"`
	Roles     []string `json:"roles" validate:"omitempty,max=10,dive,min=1,max=50"`
	Password  string   `json:"password" validate:"omitempty,min=8,max=72"`
}

type UpdateUserRequest struct {
	Name      *string `json:"name" validate:"omitempty,min=2,max=120"`
	Phone     *string `json:"phone" validate:"omitempty,max=32"`
	AvatarURL *string `json:"avatar_url" validate:"omitempty,url,max=500"`
	Status    *string `json:"status" validate:"omitempty,oneof=active inactive suspended"`
}

type AssignRolesRequest struct {
	Roles []string `json:"roles" validate:"omitempty,max=10,dive,min=1,max=50"`
}

type CreateRoleRequest struct {
	Key         string   `json:"key" validate:"required,min=2,max=50"`
	Name        string   `json:"name" validate:"required,min=2,max=80"`
	Description string   `json:"description" validate:"omitempty,max=255"`
	Permissions []string `json:"permissions" validate:"omitempty,max=50,dive,min=1,max=80"`
}

type UpdateRoleRequest struct {
	Name        *string `json:"name" validate:"omitempty,min=2,max=80"`
	Description *string `json:"description" validate:"omitempty,max=255"`
}

type SetPermissionsRequest struct {
	Permissions []string `json:"permissions" validate:"omitempty,max=50,dive,min=1,max=80"`
}

type MatrixRole struct {
	RoleID      string   `json:"role_id" validate:"required,uuid"`
	Permissions []string `json:"permissions" validate:"omitempty,max=50,dive,min=1,max=80"`
}

type AccessMatrixRequest struct {
	Roles []MatrixRole `json:"roles" validate:"required,min=1,max=50,dive"`
}

type EnsureUserRequest struct {
	ID    string `json:"id" validate:"required,uuid"`
	Email string `json:"email" validate:"required,email,max=254"`
	Name  string `json:"name" validate:"required,min=1,max=120"`
	Phone string `json:"phone" validate:"omitempty,max=32"`
	Role  string `json:"role" validate:"omitempty,max=50"`
}

type ResetPasswordResponse struct {
	TemporaryPassword string `json:"temporary_password"`
	Notice            string `json:"notice"`
}

type CreateUserResponse struct {
	Data              *User  `json:"data"`
	TemporaryPassword string `json:"temporary_password,omitempty"`
	Notice            string `json:"notice,omitempty"`
}
