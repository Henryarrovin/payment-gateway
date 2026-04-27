package data

import (
	"github.com/google/wire"
	"gorm.io/gorm"
)

var ProviderSet = wire.NewSet(
	NewDB,
	NewPermissionRepository,
	NewProviderRepository,
	NewOrderRepository,
	NewPaymentRepository,
	NewWebhookRepository,
	NewCleanup,
)

func NewCleanup(db *gorm.DB) func() {
	return func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}
}
