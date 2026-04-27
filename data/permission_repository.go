package data

import (
	"context"
	"fmt"
	"payment-gateway/models"
	"strings"

	"gorm.io/gorm"
)

// PermissionRepository fetches role-permission mappings from the DB.
// Authorization is fully data-driven: change DB rows to change access.
type PermissionRepository struct {
	db *gorm.DB
}

func NewPermissionRepository(db *gorm.DB) *PermissionRepository {
	return &PermissionRepository{db: db}
}

// IsAllowed checks if ANY of the provided roles is permitted to call the endpoint.
func (r *PermissionRepository) IsAllowed(ctx context.Context, endpoint string, userRoles []string) (bool, error) {
	var perm models.Permission
	err := r.db.WithContext(ctx).
		Where("endpoint = ?", endpoint).
		First(&perm).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, fmt.Errorf("no permission entry for endpoint %q", endpoint)
		}
		return false, fmt.Errorf("fetching permission: %w", err)
	}

	allowed := strings.Split(perm.AllowedRoles, ",")
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		allowedSet[strings.TrimSpace(r)] = struct{}{}
	}

	for _, role := range userRoles {
		if _, ok := allowedSet[role]; ok {
			return true, nil
		}
	}
	return false, nil
}

// GetPermission returns the full permission row for an endpoint.
func (r *PermissionRepository) GetPermission(ctx context.Context, endpoint string) (*models.Permission, error) {
	var perm models.Permission
	if err := r.db.WithContext(ctx).Where("endpoint = ?", endpoint).First(&perm).Error; err != nil {
		return nil, fmt.Errorf("permission not found: %w", err)
	}
	return &perm, nil
}
