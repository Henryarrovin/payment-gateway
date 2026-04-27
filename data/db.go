package data

import (
	"fmt"
	"payment-gateway/config"
	"payment-gateway/models"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func NewDB(cfg *config.Config, logger *zap.Logger) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.Database.DSN()), &gorm.Config{
		Logger: NewGormLogger(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := db.AutoMigrate(
		&models.Permission{},
		&models.ThirdPartyProvider{},
		&models.Order{},
		&models.Payment{},
		&models.Refund{},
	); err != nil {
		return nil, fmt.Errorf("auto migrating: %w", err)
	}

	seedPermissions(db, logger)
	seedProviders(db, logger)

	return db, nil
}

// seedPermissions inserts default role-based permissions.
// These are the "source of truth" for authorization — no code change needed,
// update these rows in the DB to change who can call what.
func seedPermissions(db *gorm.DB, logger *zap.Logger) {
	perms := []models.Permission{
		{
			Endpoint:     "CreateOrder",
			AllowedRoles: "user,admin",
			Description:  "Create a new payment order",
		},
		{
			Endpoint:     "CapturePayment",
			AllowedRoles: "user,admin",
			Description:  "Capture/confirm a payment after user action",
		},
		{
			Endpoint:     "GetOrder",
			AllowedRoles: "user,admin",
			Description:  "Fetch a single order by ID",
		},
		{
			Endpoint:     "ListOrders",
			AllowedRoles: "user,admin",
			Description:  "List orders for the calling user",
		},
		{
			Endpoint:     "RefundPayment",
			AllowedRoles: "admin",
			Description:  "Issue a refund — admin only",
		},
		{
			Endpoint:     "GetPayment",
			AllowedRoles: "user,admin",
			Description:  "Fetch payment details",
		},
	}

	for _, p := range perms {
		result := db.Where(models.Permission{Endpoint: p.Endpoint}).FirstOrCreate(&p)
		if result.Error != nil {
			logger.Error("failed to seed permission", zap.String("endpoint", p.Endpoint), zap.Error(result.Error))
		}
	}
	logger.Info("permissions seeded")
}

// seedProviders seeds a mock Razorpay provider.
// In production, update IsMock=false and fill real credentials.
func seedProviders(db *gorm.DB, logger *zap.Logger) {
	providers := []models.ThirdPartyProvider{
		{
			Name:      "razorpay",
			BaseURL:   "https://api.razorpay.com/v1",
			KeyID:     "rzp_test_mock_key_id",
			KeySecret: "rzp_test_mock_key_secret",
			IsActive:  true,
			IsMock:    true,
		},
	}

	for _, p := range providers {
		result := db.Where(models.ThirdPartyProvider{Name: p.Name}).FirstOrCreate(&p)
		if result.Error != nil {
			logger.Error("failed to seed provider", zap.String("name", p.Name), zap.Error(result.Error))
		}
	}
	logger.Info("providers seeded")
}
