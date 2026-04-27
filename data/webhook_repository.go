package data

import (
	"context"
	"fmt"
	"payment-gateway/models"

	"gorm.io/gorm"
)

type WebhookRepository struct {
	db *gorm.DB
}

func NewWebhookRepository(db *gorm.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

// to checks if we already handled this event (idempotency)
func (r *WebhookRepository) IsProcessed(ctx context.Context, eventID string) (bool, error) {
	var event models.WebhookEvent
	err := r.db.WithContext(ctx).
		Where("event_id = ?", eventID).
		First(&event).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking webhook event: %w", err)
	}
	return event.Processed, nil
}

// persists the raw webhook event
func (r *WebhookRepository) Save(ctx context.Context, event *models.WebhookEvent) error {
	if err := r.db.WithContext(ctx).Create(event).Error; err != nil {
		return fmt.Errorf("saving webhook event: %w", err)
	}
	return nil
}

// marks an event as successfully processed
func (r *WebhookRepository) MarkProcessed(ctx context.Context, eventID string) error {
	if err := r.db.WithContext(ctx).
		Model(&models.WebhookEvent{}).
		Where("event_id = ?", eventID).
		Update("processed", true).Error; err != nil {
		return fmt.Errorf("marking webhook processed: %w", err)
	}
	return nil
}
