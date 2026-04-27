package data

import (
	"context"
	"fmt"
	"payment-gateway/models"
	"time"

	"gorm.io/gorm"
)

type PaymentRepository struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) *PaymentRepository {
	return &PaymentRepository{db: db}
}

func (r *PaymentRepository) Create(ctx context.Context, payment *models.Payment) error {
	if err := r.db.WithContext(ctx).Create(payment).Error; err != nil {
		return fmt.Errorf("create payment: %w", err)
	}
	return nil
}

func (r *PaymentRepository) FindByID(ctx context.Context, id string) (*models.Payment, error) {
	var payment models.Payment
	err := r.db.WithContext(ctx).
		Preload("Order").
		Where("id = ?", id).
		First(&payment).Error
	if err != nil {
		return nil, fmt.Errorf("payment not found: %w", err)
	}
	return &payment, nil
}

func (r *PaymentRepository) FindByOrderID(ctx context.Context, orderID string) (*models.Payment, error) {
	var payment models.Payment
	err := r.db.WithContext(ctx).
		Where("order_id = ?", orderID).
		First(&payment).Error
	if err != nil {
		return nil, fmt.Errorf("payment not found for order: %w", err)
	}
	return &payment, nil
}

func (r *PaymentRepository) Capture(ctx context.Context, id, providerPaymentID, method string) error {
	now := time.Now()
	if err := r.db.WithContext(ctx).
		Model(&models.Payment{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":              models.OrderStatusPaid,
			"provider_payment_id": providerPaymentID,
			"method":              method,
			"captured_at":         now,
		}).Error; err != nil {
		return fmt.Errorf("capture payment: %w", err)
	}
	return nil
}

func (r *PaymentRepository) Fail(ctx context.Context, id, reason string) error {
	if err := r.db.WithContext(ctx).
		Model(&models.Payment{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":         models.OrderStatusFailed,
			"failure_reason": reason,
		}).Error; err != nil {
		return fmt.Errorf("fail payment: %w", err)
	}
	return nil
}

func (r *PaymentRepository) CreateRefund(ctx context.Context, refund *models.Refund) error {
	if err := r.db.WithContext(ctx).Create(refund).Error; err != nil {
		return fmt.Errorf("create refund: %w", err)
	}
	return nil
}

func (r *PaymentRepository) UpdateRefundStatus(ctx context.Context, providerRefundID, status string) error {
	if err := r.db.WithContext(ctx).
		Model(&models.Refund{}).
		Where("provider_refund_id = ?", providerRefundID).
		Update("status", status).Error; err != nil {
		return fmt.Errorf("update refund status: %w", err)
	}
	return nil
}
