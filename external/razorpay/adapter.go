// Adapter interface for Razorpay's payment API.
// The active provider config (URL, keys, mock flag) is fetched from the DB
package razorpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"payment-gateway/models"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type CreateOrderInput struct {
	Amount   int64
	Currency string
	Notes    string
}

type CreateOrderOutput struct {
	ProviderOrderID string
	Amount          int64
	Currency        string
	Status          string
}

type CaptureInput struct {
	ProviderOrderID   string
	ProviderPaymentID string
	ProviderSignature string
	Amount            int64
	KeySecret         string
}

type CaptureOutput struct {
	ProviderPaymentID string
	Method            string
	Status            string
}

type RefundInput struct {
	ProviderPaymentID string
	Amount            int64
	Notes             string
}

type RefundOutput struct {
	ProviderRefundID string
	Status           string
}

// Adapter is the contract every provider implementation must satisfy.
type Adapter interface {
	CreateOrder(ctx context.Context, provider *models.ThirdPartyProvider, in CreateOrderInput) (*CreateOrderOutput, error)
	CapturePayment(ctx context.Context, provider *models.ThirdPartyProvider, in CaptureInput) (*CaptureOutput, error)
	Refund(ctx context.Context, provider *models.ThirdPartyProvider, in RefundInput) (*RefundOutput, error)
}

//  Factory — returns mock or real adapter based on provider.IsMock

func NewAdapter(logger *zap.Logger) Adapter {
	// Factory always returns the router adapter which picks mock vs real at runtime.
	return &routerAdapter{
		mock: &MockAdapter{logger: logger},
		real: &RealAdapter{logger: logger},
	}
}

type routerAdapter struct {
	mock Adapter
	real Adapter
}

func (r *routerAdapter) CreateOrder(ctx context.Context, p *models.ThirdPartyProvider, in CreateOrderInput) (*CreateOrderOutput, error) {
	if p.IsMock {
		return r.mock.CreateOrder(ctx, p, in)
	}
	return r.real.CreateOrder(ctx, p, in)
}

func (r *routerAdapter) CapturePayment(ctx context.Context, p *models.ThirdPartyProvider, in CaptureInput) (*CaptureOutput, error) {
	if p.IsMock {
		return r.mock.CapturePayment(ctx, p, in)
	}
	return r.real.CapturePayment(ctx, p, in)
}

func (r *routerAdapter) Refund(ctx context.Context, p *models.ThirdPartyProvider, in RefundInput) (*RefundOutput, error) {
	if p.IsMock {
		return r.mock.Refund(ctx, p, in)
	}
	return r.real.Refund(ctx, p, in)
}

//  MockAdapter — simulates Razorpay responses locally

type MockAdapter struct {
	logger *zap.Logger
}

func (m *MockAdapter) CreateOrder(ctx context.Context, p *models.ThirdPartyProvider, in CreateOrderInput) (*CreateOrderOutput, error) {
	m.logger.Info("mock.razorpay.CreateOrder",
		zap.String("provider", p.Name),
		zap.Int64("amount", in.Amount),
		zap.String("currency", in.Currency),
	)

	// Simulate provider-side order ID (Razorpay format: order_<random>)
	providerOrderID := fmt.Sprintf("order_mock_%s", uuid.New().String()[:8])

	return &CreateOrderOutput{
		ProviderOrderID: providerOrderID,
		Amount:          in.Amount,
		Currency:        in.Currency,
		Status:          "created",
	}, nil
}

func (m *MockAdapter) CapturePayment(ctx context.Context, p *models.ThirdPartyProvider, in CaptureInput) (*CaptureOutput, error) {
	m.logger.Info("mock.razorpay.CapturePayment",
		zap.String("provider", p.Name),
		zap.String("provider_order_id", in.ProviderOrderID),
		zap.String("provider_payment_id", in.ProviderPaymentID),
	)

	// In mock mode, skip real signature verification.
	// In real mode (RealAdapter), we verify HMAC-SHA256.
	_ = in.ProviderSignature

	return &CaptureOutput{
		ProviderPaymentID: in.ProviderPaymentID,
		Method:            "mock_upi",
		Status:            "captured",
	}, nil
}

func (m *MockAdapter) Refund(ctx context.Context, p *models.ThirdPartyProvider, in RefundInput) (*RefundOutput, error) {
	m.logger.Info("mock.razorpay.Refund",
		zap.String("provider", p.Name),
		zap.String("provider_payment_id", in.ProviderPaymentID),
		zap.Int64("amount", in.Amount),
	)

	refundID := fmt.Sprintf("rfnd_mock_%s", uuid.New().String()[:8])

	return &RefundOutput{
		ProviderRefundID: refundID,
		Status:           "processed",
	}, nil
}

//  RealAdapter — actual Razorpay HTTP calls
//  stubbed with the real Razorpay API contract

type RealAdapter struct {
	logger *zap.Logger
}

func (r *RealAdapter) CreateOrder(ctx context.Context, p *models.ThirdPartyProvider, in CreateOrderInput) (*CreateOrderOutput, error) {
	r.logger.Info("real.razorpay.CreateOrder — would POST to Razorpay API",
		zap.String("base_url", p.BaseURL),
		zap.Int64("amount", in.Amount),
	)
	// TODO: implement real HTTP call:
	//   POST {p.BaseURL}/orders
	//   Basic auth: p.KeyID : p.KeySecret
	//   Body: {"amount": in.Amount, "currency": in.Currency, "receipt": uuid}
	return nil, fmt.Errorf("real Razorpay adapter not yet configured — set IsMock=true in DB for development")
}

func (r *RealAdapter) CapturePayment(ctx context.Context, p *models.ThirdPartyProvider, in CaptureInput) (*CaptureOutput, error) {
	// Verify Razorpay signature: HMAC-SHA256(order_id + "|" + payment_id, key_secret)
	mac := hmac.New(sha256.New, []byte(p.KeySecret))
	mac.Write([]byte(fmt.Sprintf("%s|%s", in.ProviderOrderID, in.ProviderPaymentID)))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(in.ProviderSignature)) {
		return nil, fmt.Errorf("razorpay signature verification failed")
	}

	// TODO: optionally call Razorpay capture API for manual capture orders.
	return &CaptureOutput{
		ProviderPaymentID: in.ProviderPaymentID,
		Method:            "unknown",
		Status:            "captured",
	}, nil
}

func (r *RealAdapter) Refund(ctx context.Context, p *models.ThirdPartyProvider, in RefundInput) (*RefundOutput, error) {
	// TODO: POST {p.BaseURL}/payments/{in.ProviderPaymentID}/refund
	return nil, fmt.Errorf("real Razorpay adapter not yet configured")
}

func MockTime() string {
	return time.Now().UTC().Format(time.RFC3339)
}
