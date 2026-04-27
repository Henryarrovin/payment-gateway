package data

import (
	"context"
	"fmt"
	"payment-gateway/models"

	"gorm.io/gorm"
)

type OrderRepository struct {
	db *gorm.DB
}

func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

func (r *OrderRepository) Create(ctx context.Context, order *models.Order) error {
	if err := r.db.WithContext(ctx).Create(order).Error; err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	return nil
}

func (r *OrderRepository) FindByID(ctx context.Context, id string) (*models.Order, error) {
	var order models.Order
	err := r.db.WithContext(ctx).
		Preload("Provider").
		Where("id = ?", id).
		First(&order).Error
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}
	return &order, nil
}

func (r *OrderRepository) ListByUserID(ctx context.Context, userID string) ([]models.Order, error) {
	var orders []models.Order
	if err := r.db.WithContext(ctx).
		Preload("Provider").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&orders).Error; err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	return orders, nil
}

func (r *OrderRepository) UpdateStatus(ctx context.Context, id string, status models.OrderStatus) error {
	if err := r.db.WithContext(ctx).
		Model(&models.Order{}).
		Where("id = ?", id).
		Update("status", status).Error; err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	return nil
}

func (r *OrderRepository) UpdateProviderOrderID(ctx context.Context, id, providerOrderID string) error {
	if err := r.db.WithContext(ctx).
		Model(&models.Order{}).
		Where("id = ?", id).
		Update("provider_order_id", providerOrderID).Error; err != nil {
		return fmt.Errorf("update provider order id: %w", err)
	}
	return nil
}

func (r *OrderRepository) FindByProviderOrderID(ctx context.Context, providerOrderID string) (*models.Order, error) {
	var order models.Order
	err := r.db.WithContext(ctx).
		Preload("Provider").
		Where("provider_order_id = ?", providerOrderID).
		First(&order).Error
	if err != nil {
		return nil, fmt.Errorf("order not found by provider_order_id: %w", err)
	}
	return &order, nil
}
