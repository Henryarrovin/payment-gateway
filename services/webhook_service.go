package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"payment-gateway/data"
	"payment-gateway/models"

	"go.uber.org/zap"
)

// RazorpayWebhookPayload mirrors Razorpay's webhook body structure
type RazorpayWebhookPayload struct {
	Entity    string `json:"entity"`
	AccountID string `json:"account_id"`
	Event     string `json:"event"` // payment.captured, payment.failed, refund.processed
	Payload   struct {
		Payment *struct {
			Entity struct {
				ID      string `json:"id"`
				OrderID string `json:"order_id"`
				Status  string `json:"status"`
				Method  string `json:"method"`
				Amount  int64  `json:"amount"`
			} `json:"entity"`
		} `json:"payment"`
		Refund *struct {
			Entity struct {
				ID        string `json:"id"`
				PaymentID string `json:"payment_id"`
				Amount    int64  `json:"amount"`
				Status    string `json:"status"`
			} `json:"entity"`
		} `json:"refund"`
	} `json:"payload"`
}

type WebhookService struct {
	webhooks *data.WebhookRepository
	orders   *data.OrderRepository
	payments *data.PaymentRepository
	logger   *zap.Logger
}

func NewWebhookService(
	webhooks *data.WebhookRepository,
	orders *data.OrderRepository,
	payments *data.PaymentRepository,
	logger *zap.Logger,
) *WebhookService {
	return &WebhookService{
		webhooks: webhooks,
		orders:   orders,
		payments: payments,
		logger:   logger,
	}
}

// VerifySignature validates the Razorpay webhook signature
// Razorpay signs with: HMAC-SHA256(raw_body, webhook_secret)
func (s *WebhookService) VerifySignature(rawBody []byte, signature, webhookSecret string) bool {
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// Process handles a verified incoming webhook event
func (s *WebhookService) Process(ctx context.Context, eventID, event string, rawBody []byte) error {
	log := s.logger

	processed, err := s.webhooks.IsProcessed(ctx, eventID)
	if err != nil {
		return fmt.Errorf("idempotency check failed: %w", err)
	}
	if processed {
		log.Info("webhook.already_processed", zap.String("event_id", eventID))
		return nil
	}

	webhookEvent := &models.WebhookEvent{
		EventID:   eventID,
		Event:     event,
		Payload:   string(rawBody),
		Processed: false,
	}
	if err := s.webhooks.Save(ctx, webhookEvent); err != nil {
		return fmt.Errorf("saving webhook event: %w", err)
	}

	var payload RazorpayWebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return fmt.Errorf("parsing webhook payload: %w", err)
	}

	switch event {
	case "payment.captured":
		if err := s.handlePaymentCaptured(ctx, payload); err != nil {
			log.Error("webhook.payment_captured.failed", zap.Error(err))
			return err
		}

	case "payment.failed":
		if err := s.handlePaymentFailed(ctx, payload); err != nil {
			log.Error("webhook.payment_failed.failed", zap.Error(err))
			return err
		}

	case "refund.processed":
		if err := s.handleRefundProcessed(ctx, payload); err != nil {
			log.Error("webhook.refund_processed.failed", zap.Error(err))
			return err
		}

	default:
		log.Info("webhook.unhandled_event", zap.String("event", event))
	}

	if err := s.webhooks.MarkProcessed(ctx, eventID); err != nil {
		return fmt.Errorf("marking processed: %w", err)
	}

	log.Info("webhook.processed",
		zap.String("event_id", eventID),
		zap.String("event", event),
	)
	return nil
}

func (s *WebhookService) handlePaymentCaptured(ctx context.Context, payload RazorpayWebhookPayload) error {
	if payload.Payload.Payment == nil {
		return fmt.Errorf("missing payment entity in payload")
	}

	p := payload.Payload.Payment.Entity
	s.logger.Info("webhook.payment_captured",
		zap.String("provider_payment_id", p.ID),
		zap.String("provider_order_id", p.OrderID),
	)

	order, err := s.orders.FindByProviderOrderID(ctx, p.OrderID)
	if err != nil {
		return fmt.Errorf("order not found for provider_order_id %s: %w", p.OrderID, err)
	}

	payment, err := s.payments.FindByOrderID(ctx, order.ID)
	if err != nil {
		return fmt.Errorf("payment not found for order %s: %w", order.ID, err)
	}

	if payment.Status == models.OrderStatusPending {
		if err := s.payments.Capture(ctx, payment.ID, p.ID, p.Method); err != nil {
			return fmt.Errorf("capturing payment: %w", err)
		}
		if err := s.orders.UpdateStatus(ctx, order.ID, models.OrderStatusPaid); err != nil {
			return fmt.Errorf("updating order status: %w", err)
		}
	}
	return nil
}

func (s *WebhookService) handlePaymentFailed(ctx context.Context, payload RazorpayWebhookPayload) error {
	if payload.Payload.Payment == nil {
		return fmt.Errorf("missing payment entity in payload")
	}

	p := payload.Payload.Payment.Entity
	s.logger.Info("webhook.payment_failed",
		zap.String("provider_payment_id", p.ID),
		zap.String("provider_order_id", p.OrderID),
	)

	order, err := s.orders.FindByProviderOrderID(ctx, p.OrderID)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}

	payment, err := s.payments.FindByOrderID(ctx, order.ID)
	if err != nil {
		return fmt.Errorf("payment not found: %w", err)
	}

	if payment.Status == models.OrderStatusPending {
		if err := s.payments.Fail(ctx, payment.ID, "payment failed via webhook"); err != nil {
			return fmt.Errorf("failing payment: %w", err)
		}
		if err := s.orders.UpdateStatus(ctx, order.ID, models.OrderStatusFailed); err != nil {
			return fmt.Errorf("updating order status: %w", err)
		}
	}
	return nil
}

func (s *WebhookService) handleRefundProcessed(ctx context.Context, payload RazorpayWebhookPayload) error {
	if payload.Payload.Refund == nil {
		return fmt.Errorf("missing refund entity in payload")
	}

	ref := payload.Payload.Refund.Entity
	s.logger.Info("webhook.refund_processed",
		zap.String("provider_refund_id", ref.ID),
		zap.String("provider_payment_id", ref.PaymentID),
	)

	if err := s.payments.UpdateRefundStatus(ctx, ref.ID, ref.Status); err != nil {
		return fmt.Errorf("updating refund status: %w", err)
	}
	return nil
}
