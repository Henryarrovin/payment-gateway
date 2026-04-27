package models

import (
	"time"
)

// ─────────────────────────────────────────────
//  Permission table
//  Controls which roles can call which endpoint.
//  No code change needed — just update DB inserts.
//
//  Example rows:
//    endpoint="CreateOrder"  allowed_roles=["user","admin"]
//    endpoint="RefundOrder"  allowed_roles=["admin"]
//    endpoint="ListOrders"   allowed_roles=["user","admin"]
// ─────────────────────────────────────────────

type Permission struct {
	ID           string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Endpoint     string `gorm:"uniqueIndex;not null"` // matches gRPC method name
	AllowedRoles string `gorm:"not null"`             // comma-separated: "user,admin"
	Description  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ThirdPartyProvider struct {
	ID            string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name          string `gorm:"uniqueIndex;not null"` // "razorpay", "stripe"
	BaseURL       string `gorm:"not null"`
	KeyID         string `gorm:"not null"`
	KeySecret     string `gorm:"not null"`
	WebhookSecret string
	IsActive      bool `gorm:"default:true"`
	IsMock        bool `gorm:"default:false"` // if true, use mock adapter
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusFailed    OrderStatus = "failed"
	OrderStatusRefunded  OrderStatus = "refunded"
	OrderStatusCancelled OrderStatus = "cancelled"
)

type Order struct {
	ID              string             `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID          string             `gorm:"not null;index"`
	ProviderID      string             `gorm:"type:uuid;not null"`
	Provider        ThirdPartyProvider `gorm:"foreignKey:ProviderID"`
	ProviderOrderID string             `gorm:"index"`    // Razorpay order id
	Amount          int64              `gorm:"not null"` // paise / cents
	Currency        string             `gorm:"not null;default:'INR'"`
	Status          OrderStatus        `gorm:"not null;default:'pending'"`
	Notes           string             // JSON blob for extra metadata
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Payment struct {
	ID                string      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	OrderID           string      `gorm:"type:uuid;not null;index"`
	Order             Order       `gorm:"foreignKey:OrderID"`
	ProviderPaymentID string      `gorm:"index"` // Razorpay payment_id
	Method            string      // "upi","card","netbanking"
	Status            OrderStatus `gorm:"not null;default:'pending'"`
	FailureReason     string
	CapturedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Refund struct {
	ID               string  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	PaymentID        string  `gorm:"type:uuid;not null;index"`
	Payment          Payment `gorm:"foreignKey:PaymentID"`
	ProviderRefundID string  `gorm:"index"`
	Amount           int64   `gorm:"not null"`
	Status           string  `gorm:"not null;default:'pending'"` // "pending","processed","failed"
	Notes            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type WebhookEvent struct {
	ID        string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EventID   string `gorm:"uniqueIndex;not null"` // razorpay event id — prevents duplicate processing
	Event     string `gorm:"not null"`             // payment.captured, payment.failed, refund.processed
	Payload   string `gorm:"type:text;not null"`   // raw JSON body
	Processed bool   `gorm:"default:false"`
	CreatedAt time.Time
}
