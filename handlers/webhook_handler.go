package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"payment-gateway/data"
	"payment-gateway/services"

	"go.uber.org/zap"
)

type WebhookHandler struct {
	svc       *services.WebhookService
	providers *data.ProviderRepository
	logger    *zap.Logger
}

func NewWebhookHandler(
	svc *services.WebhookService,
	providers *data.ProviderRepository,
	logger *zap.Logger,
) *WebhookHandler {
	return &WebhookHandler{svc: svc, providers: providers, logger: logger}
}

// ServeHTTP handles POST /api/v1/payments/webhook
// No auth middleware — Razorpay calls this directly, verified by HMAC signature
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("webhook.read_body_failed", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	signature := r.Header.Get("X-Razorpay-Signature")
	eventID := r.Header.Get("X-Razorpay-Event-Id")

	if signature == "" || eventID == "" {
		h.logger.Warn("webhook.missing_headers",
			zap.String("signature", signature),
			zap.String("event_id", eventID),
		)
		http.Error(w, "missing razorpay headers", http.StatusBadRequest)
		return
	}

	// Fetch provider webhook secret from DB
	provider, err := h.providers.GetActiveProvider(r.Context())
	if err != nil {
		h.logger.Error("webhook.provider_fetch_failed", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Verify HMAC signature
	if !h.svc.VerifySignature(rawBody, signature, provider.WebhookSecret) {
		h.logger.Warn("webhook.invalid_signature", zap.String("event_id", eventID))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Extract event type from body
	var eventBody struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(rawBody, &eventBody); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Process async so Razorpay gets 200 immediately
	// Razorpay retries if it doesn't get 200 within 5s
	go func() {
		bgCtx := context.Background()
		if err := h.svc.Process(bgCtx, eventID, eventBody.Event, rawBody); err != nil {
			h.logger.Error("webhook.process_failed",
				zap.String("event_id", eventID),
				zap.String("event", eventBody.Event),
				zap.Error(err),
			)
		}
	}()

	// Always return 200 immediately
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
