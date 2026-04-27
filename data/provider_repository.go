package data

import (
	"context"
	"fmt"
	"payment-gateway/models"

	"gorm.io/gorm"
)

// ProviderRepository fetches payment provider configs from the DB.
// Swap providers by toggling is_active or changing the name column.
type ProviderRepository struct {
	db *gorm.DB
}

func NewProviderRepository(db *gorm.DB) *ProviderRepository {
	return &ProviderRepository{db: db}
}

// GetActiveProvider returns the currently active payment provider.
func (r *ProviderRepository) GetActiveProvider(ctx context.Context) (*models.ThirdPartyProvider, error) {
	var provider models.ThirdPartyProvider
	err := r.db.WithContext(ctx).
		Where("is_active = true").
		First(&provider).Error
	if err != nil {
		return nil, fmt.Errorf("no active provider found: %w", err)
	}
	return &provider, nil
}

// GetProviderByName fetches a specific provider by name.
func (r *ProviderRepository) GetProviderByName(ctx context.Context, name string) (*models.ThirdPartyProvider, error) {
	var provider models.ThirdPartyProvider
	if err := r.db.WithContext(ctx).Where("name = ? AND is_active = true", name).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", name, err)
	}
	return &provider, nil
}
