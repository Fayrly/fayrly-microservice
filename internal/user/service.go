package user

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
	"github.com/fayrly/backend/internal/platform/password"
	"github.com/fayrly/backend/internal/user/authclient"
)

var roleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,49}$`)

// CredentialProvisioner is implemented by authclient.Client.
type CredentialProvisioner interface {
	CreateCredential(ctx context.Context, input authclient.CreateCredentialInput) (string, error)
	DeleteCredential(ctx context.Context, userID string) error
	ResetPassword(ctx context.Context, userID string) (string, error)
}

type Service struct {
	repo   *Repository
	auth   CredentialProvisioner
	logger *slog.Logger
}

func NewService(repo *Repository, auth CredentialProvisioner, logger *slog.Logger) *Service {
	return &Service{repo: repo, auth: auth, logger: logger}
}

func (s *Service) ListUsers(ctx context.Context, filter UserFilter, page httpx.Page) ([]*User, int, error) {
	if filter.Status != "" && !validStatus(filter.Status) {
		return nil, 0, apierr.Validation(map[string]string{"status": "must be one of: active, inactive, suspended"})
	}
	users, total, err := s.repo.ListUsers(ctx, filter, page.Size, page.Offset())
	if err != nil {
		return nil, 0, mapError(err)
	}
	return users, total, nil
}

func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	if !validUUID(id) {
		return nil, apierr.NotFound("user")
	}
	u, err := s.repo.GetUser(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	return u, nil
}

func (s *Service) CreateUser(ctx context.Context, actor *authn.Principal, req CreateUserRequest) (*CreateUserResponse, error) {
	email := password.NormalizeEmail(req.Email)
	roles := dedupe(req.Roles)
	if len(roles) == 0 {
		roles = []string{RoleMember}
	}
	if contains(roles, RoleSuperadmin) && !actor.IsSuperadmin() {
		return nil, apierr.Forbidden("only a superadmin can assign the superadmin role")
	}
	if err := s.validateRoles(ctx, roles); err != nil {
		return nil, err
	}
	if req.Password != "" {
		if err := password.Validate(req.Password); err != nil {
			return nil, apierr.Validation(map[string]string{"password": err.Error()})
		}
	}

	u := &User{
		ID:        uuid.NewString(),
		Email:     email,
		Name:      strings.TrimSpace(req.Name),
		Phone:     strings.TrimSpace(req.Phone),
		AvatarURL: strings.TrimSpace(req.AvatarURL),
		Status:    StatusActive,
	}
	if err := s.repo.CreateUser(ctx, u, roles, actor.UserID); err != nil {
		return nil, mapError(err)
	}

	temporaryPassword, err := s.auth.CreateCredential(ctx, authclient.CreateCredentialInput{
		UserID:   u.ID,
		Email:    email,
		Password: req.Password,
	})
	if err != nil {
		if cleanupErr := s.repo.HardDeleteUser(ctx, u.ID); cleanupErr != nil {
			s.logger.Error("failed to roll back user after credential provisioning failure",
				"user_id", u.ID, "error", cleanupErr)
		}
		return nil, err
	}

	created, err := s.repo.GetUser(ctx, u.ID)
	if err != nil {
		return nil, mapError(err)
	}

	response := &CreateUserResponse{Data: created, TemporaryPassword: temporaryPassword}
	if temporaryPassword == "" {
		response.Notice = "The administrator supplied the initial password. The user should change it after signing in."
	} else {
		response.Notice = "Share this temporary password securely. It is shown only once."
	}
	return response, nil
}

func (s *Service) UpdateUser(ctx context.Context, actor *authn.Principal, id string, req UpdateUserRequest) (*User, error) {
	existing, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Status != nil {
		status := *req.Status
		if id == actor.UserID && status != StatusActive {
			return nil, apierr.BadRequest("you cannot change the status of your own account")
		}
		if status != StatusActive && contains(existing.Roles, RoleSuperadmin) {
			if err := s.ensureAnotherSuperadmin(ctx, id); err != nil {
				return nil, err
			}
		}
	}

	patch := UserPatch{Name: req.Name, Phone: req.Phone, AvatarURL: req.AvatarURL, Status: req.Status}
	if err := s.repo.UpdateUser(ctx, id, patch); err != nil {
		return nil, mapError(err)
	}
	return s.GetUser(ctx, id)
}

func (s *Service) DeleteUser(ctx context.Context, actor *authn.Principal, id string) error {
	existing, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if id == actor.UserID {
		return apierr.BadRequest("you cannot delete your own account")
	}
	if contains(existing.Roles, RoleSuperadmin) {
		if err := s.ensureAnotherSuperadmin(ctx, id); err != nil {
			return err
		}
	}

	if err := s.auth.DeleteCredential(ctx, id); err != nil {
		return err
	}
	if err := s.repo.SoftDeleteUser(ctx, id); err != nil {
		return mapError(err)
	}
	return nil
}

func (s *Service) AssignRoles(ctx context.Context, actor *authn.Principal, id string, roleKeys []string) (*User, error) {
	existing, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	roleKeys = dedupe(roleKeys)
	if err := s.validateRoles(ctx, roleKeys); err != nil {
		return nil, err
	}

	if !actor.IsSuperadmin() && (contains(existing.Roles, RoleSuperadmin) || contains(roleKeys, RoleSuperadmin)) {
		return nil, apierr.Forbidden("only a superadmin can change the superadmin role assignment")
	}

	losesSuperadmin := contains(existing.Roles, RoleSuperadmin) &&
		!contains(roleKeys, RoleSuperadmin) && existing.Status == StatusActive
	if losesSuperadmin {
		if err := s.ensureAnotherSuperadmin(ctx, id); err != nil {
			return nil, err
		}
	}

	if err := s.repo.SetUserRoles(ctx, id, roleKeys, actor.UserID); err != nil {
		return nil, mapError(err)
	}
	return s.repo.GetUser(ctx, id)
}

func (s *Service) ResetPassword(ctx context.Context, id string) (*ResetPasswordResponse, error) {
	if _, err := s.GetUser(ctx, id); err != nil {
		return nil, err
	}
	temporaryPassword, err := s.auth.ResetPassword(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ResetPasswordResponse{
		TemporaryPassword: temporaryPassword,
		Notice:            "All existing sessions were revoked. Share the temporary password securely.",
	}, nil
}

// --- Roles ----------------------------------------------------------------

func (s *Service) ListRoles(ctx context.Context) ([]*Role, error) {
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return roles, nil
}

func (s *Service) GetRole(ctx context.Context, id string) (*Role, error) {
	if !validUUID(id) {
		return nil, apierr.NotFound("role")
	}
	role, err := s.repo.GetRole(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	return role, nil
}

func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	permissions, err := s.repo.ListPermissions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return permissions, nil
}

func (s *Service) AccessMatrix(ctx context.Context) (*AccessMatrix, error) {
	permissions, err := s.repo.ListPermissions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return &AccessMatrix{Permissions: permissions, Roles: roles}, nil
}

func (s *Service) CreateRole(ctx context.Context, req CreateRoleRequest) (*Role, error) {
	key := strings.ToLower(strings.TrimSpace(req.Key))
	if !roleKeyPattern.MatchString(key) {
		return nil, apierr.Validation(map[string]string{
			"key": "must start with a letter and contain only lowercase letters, numbers, dashes and underscores",
		})
	}
	permissions, err := s.validatePermissions(ctx, req.Permissions)
	if err != nil {
		return nil, err
	}

	role := &Role{
		ID:          uuid.NewString(),
		Key:         key,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
	}
	if err := s.repo.CreateRole(ctx, role, permissions); err != nil {
		return nil, mapError(err)
	}
	return s.GetRole(ctx, role.ID)
}

func (s *Service) UpdateRole(ctx context.Context, id string, req UpdateRoleRequest) (*Role, error) {
	role, err := s.GetRole(ctx, id)
	if err != nil {
		return nil, err
	}
	if role.Key == RoleSuperadmin {
		return nil, apierr.Conflict(ErrSuperadminLocked.Error())
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		req.Name = &trimmed
	}
	if req.Description != nil {
		trimmed := strings.TrimSpace(*req.Description)
		req.Description = &trimmed
	}
	if err := s.repo.UpdateRole(ctx, id, req); err != nil {
		return nil, mapError(err)
	}
	return s.GetRole(ctx, id)
}

func (s *Service) DeleteRole(ctx context.Context, id string) error {
	role, err := s.GetRole(ctx, id)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return apierr.Conflict("system roles cannot be deleted")
	}
	count, err := s.repo.CountRoleUsers(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if count > 0 {
		return apierr.Conflict("this role is still assigned to users; reassign them first")
	}
	if err := s.repo.DeleteRole(ctx, id); err != nil {
		return mapError(err)
	}
	return nil
}

func (s *Service) SetRolePermissions(ctx context.Context, id string, permissions []string) (*Role, error) {
	role, err := s.GetRole(ctx, id)
	if err != nil {
		return nil, err
	}
	if role.Key == RoleSuperadmin {
		return nil, apierr.Conflict(ErrSuperadminLocked.Error())
	}
	permissions, err = s.validatePermissions(ctx, permissions)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetRolePermissions(ctx, id, permissions); err != nil {
		return nil, mapError(err)
	}
	return s.GetRole(ctx, id)
}

func (s *Service) SetAccessMatrix(ctx context.Context, req AccessMatrixRequest) (*AccessMatrix, error) {
	rolesByID := make(map[string]*Role)
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	for _, role := range roles {
		rolesByID[role.ID] = role
	}

	seen := make(map[string]bool, len(req.Roles))
	var updates []RolePermissionsUpdate
	for _, entry := range req.Roles {
		if seen[entry.RoleID] {
			return nil, apierr.Validation(map[string]string{"roles": "each role may only appear once"})
		}
		seen[entry.RoleID] = true

		role, ok := rolesByID[entry.RoleID]
		if !ok {
			return nil, apierr.NotFound("role")
		}
		permissions, err := s.validatePermissions(ctx, entry.Permissions)
		if err != nil {
			return nil, err
		}
		if role.Key == RoleSuperadmin {
			if !sameSet(role.Permissions, permissions) {
				return nil, apierr.Conflict(ErrSuperadminLocked.Error())
			}
			continue
		}
		updates = append(updates, RolePermissionsUpdate{RoleID: entry.RoleID, Permissions: permissions})
	}

	if err := s.repo.SetAccessMatrix(ctx, updates); err != nil {
		return nil, mapError(err)
	}
	return s.AccessMatrix(ctx)
}

// --- Internal API ---------------------------------------------------------

func (s *Service) EnsureUser(ctx context.Context, req EnsureUserRequest) (*User, error) {
	email := password.NormalizeEmail(req.Email)
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = RoleMember
	}
	if err := s.validateRoles(ctx, []string{role}); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetUser(ctx, req.ID)
	switch {
	case err == nil:
		if !contains(existing.Roles, role) {
			if err := s.repo.AddUserRole(ctx, existing.ID, role); err != nil {
				return nil, mapError(err)
			}
			return s.repo.GetUser(ctx, existing.ID)
		}
		return existing, nil
	case !errors.Is(err, ErrUserNotFound):
		return nil, mapError(err)
	}

	deleted, err := s.repo.IsDeleted(ctx, req.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if deleted {
		return nil, apierr.Conflict(ErrDeletedUser.Error())
	}

	if _, err := s.repo.GetUserByEmail(ctx, email); err == nil {
		return nil, apierr.Conflict(ErrEmailTaken.Error())
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, mapError(err)
	}

	u := &User{
		ID:     req.ID,
		Email:  email,
		Name:   strings.TrimSpace(req.Name),
		Phone:  strings.TrimSpace(req.Phone),
		Status: StatusActive,
	}
	if err := s.repo.CreateUser(ctx, u, []string{role}, ""); err != nil {
		return nil, mapError(err)
	}
	return s.repo.GetUser(ctx, req.ID)
}

func (s *Service) GetUserByEmailInternal(ctx context.Context, email string) (*User, error) {
	u, err := s.repo.GetUserByEmail(ctx, password.NormalizeEmail(email))
	if err != nil {
		return nil, mapError(err)
	}
	return u, nil
}

func (s *Service) GetUserInternal(ctx context.Context, id string) (*User, error) {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// --- helpers --------------------------------------------------------------

func (s *Service) validateRoles(ctx context.Context, roleKeys []string) error {
	if len(roleKeys) == 0 {
		return nil
	}
	roles, err := s.repo.RoleIDsByKey(ctx)
	if err != nil {
		return mapError(err)
	}
	for _, key := range roleKeys {
		if _, ok := roles[key]; !ok {
			return apierr.Validation(map[string]string{"roles": fmt.Sprintf("unknown role %q", key)})
		}
	}
	return nil
}

func (s *Service) validatePermissions(ctx context.Context, permissions []string) ([]string, error) {
	permissions = dedupe(permissions)
	known, err := s.repo.PermissionKeys(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	var unknown []string
	for _, permission := range permissions {
		if !known[permission] {
			unknown = append(unknown, permission)
		}
	}
	if len(unknown) > 0 {
		return nil, apierr.Validation(map[string]string{
			"permissions": "unknown permission(s): " + strings.Join(unknown, ", "),
		})
	}
	return permissions, nil
}

func (s *Service) ensureAnotherSuperadmin(ctx context.Context, excludeID string) error {
	count, err := s.repo.CountActiveSuperadmins(ctx, excludeID)
	if err != nil {
		return mapError(err)
	}
	if count == 0 {
		return apierr.Conflict("at least one active superadmin must remain")
	}
	return nil
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	switch {
	case errors.Is(err, ErrUserNotFound):
		return apierr.NotFound("user")
	case errors.Is(err, ErrDeletedUser):
		return apierr.New(http.StatusGone, "user_deleted", err.Error())
	case errors.Is(err, ErrEmailTaken):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrRoleNotFound):
		return apierr.NotFound("role")
	case errors.Is(err, ErrRoleExists):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrRoleInUse):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrSystemRole):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrSuperadminLocked):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrLastSuperadmin):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrPermissionNotFound):
		return apierr.Validation(map[string]string{"permissions": "one or more permissions do not exist"})
	default:
		return err
	}
}

func validStatus(status string) bool {
	switch status {
	case StatusActive, StatusInactive, StatusSuspended:
		return true
	}
	return false
}

func validUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func dedupe(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sameSet(a, b []string) bool {
	left := dedupe(a)
	right := dedupe(b)
	if len(left) != len(right) {
		return false
	}
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
