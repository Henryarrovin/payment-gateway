package razorpay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"payment-gateway/models"
	"time"

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

// Adapter interface

type Adapter interface {
	CreateOrder(ctx context.Context, provider *models.ThirdPartyProvider, in CreateOrderInput) (*CreateOrderOutput, error)
	CapturePayment(ctx context.Context, provider *models.ThirdPartyProvider, in CaptureInput) (*CaptureOutput, error)
	Refund(ctx context.Context, provider *models.ThirdPartyProvider, in RefundInput) (*RefundOutput, error)
}

// Factory
// For local dev, DB provider row points base_url to http://localhost:8090/v1
// For production, DB provider row points base_url to https://api.razorpay.com/v1

func NewAdapter(logger *zap.Logger) Adapter {
	return &RealAdapter{
		logger:     logger,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type RealAdapter struct {
	logger     *zap.Logger
	httpClient *http.Client
}

func (r *RealAdapter) CreateOrder(ctx context.Context, p *models.ThirdPartyProvider, in CreateOrderInput) (*CreateOrderOutput, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"amount":   in.Amount,
		"currency": in.Currency,
		"receipt":  fmt.Sprintf("rcpt_%d", time.Now().UnixNano()),
		"notes":    in.Notes,
	})

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		p.BaseURL+"/orders",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create order request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(p.KeyID, p.KeySecret)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create order call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create order: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		ID       string `json:"id"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode create order response: %w", err)
	}

	r.logger.Info("razorpay.create_order",
		zap.String("provider_order_id", result.ID),
		zap.String("base_url", p.BaseURL),
		zap.Int64("amount", result.Amount),
	)

	return &CreateOrderOutput{
		ProviderOrderID: result.ID,
		Amount:          result.Amount,
		Currency:        result.Currency,
		Status:          result.Status,
	}, nil
}

func (r *RealAdapter) CapturePayment(ctx context.Context, p *models.ThirdPartyProvider, in CaptureInput) (*CaptureOutput, error) {
	// Verify HMAC-SHA256 signature from frontend/mock
	// Razorpay signs: order_id|payment_id with key_secret
	mac := hmac.New(sha256.New, []byte(p.KeySecret))
	mac.Write([]byte(fmt.Sprintf("%s|%s", in.ProviderOrderID, in.ProviderPaymentID)))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(in.ProviderSignature)) {
		return nil, fmt.Errorf("razorpay signature verification failed")
	}

	// Call provider capture endpoint
	body, _ := json.Marshal(map[string]interface{}{
		"amount":   in.Amount,
		"order_id": in.ProviderOrderID, // sent so mock can reference it in webhook
	})

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		fmt.Sprintf("%s/payments/%s/capture", p.BaseURL, in.ProviderPaymentID),
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("capture request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(p.KeyID, p.KeySecret)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("capture call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("capture: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode capture response: %w", err)
	}

	r.logger.Info("razorpay.capture_payment",
		zap.String("provider_payment_id", result.ID),
		zap.String("method", result.Method),
		zap.String("status", result.Status),
	)

	return &CaptureOutput{
		ProviderPaymentID: result.ID,
		Method:            result.Method,
		Status:            result.Status,
	}, nil
}

func (r *RealAdapter) Refund(ctx context.Context, p *models.ThirdPartyProvider, in RefundInput) (*RefundOutput, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"amount": in.Amount,
		"notes":  in.Notes,
	})

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		fmt.Sprintf("%s/payments/%s/refund", p.BaseURL, in.ProviderPaymentID),
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("refund request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(p.KeyID, p.KeySecret)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refund call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refund: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode refund response: %w", err)
	}

	r.logger.Info("razorpay.refund",
		zap.String("provider_refund_id", result.ID),
		zap.String("status", result.Status),
	)

	return &RefundOutput{
		ProviderRefundID: result.ID,
		Status:           result.Status,
	}, nil
}
