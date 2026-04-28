package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// webhookTarget is where the mock fires webhooks automatically
const webhookTarget = "http://localhost:8081/api/v1/payments/webhook"

// webhookSecret must match the webhook_secret in the DB provider row
const webhookSecret = "rzp_webhook_mock_secret"

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/orders", handleCreateOrder)
	mux.HandleFunc("/v1/payments/", handlePayments)
	mux.HandleFunc("/v1/sign", handleSign)

	log.Println("mock-razorpay listening on :8090")
	if err := http.ListenAndServe(":8090", loggingMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[mock-razorpay] %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func basicAuth(r *http.Request) bool {
	_, _, ok := r.BasicAuth()
	return ok
}

// ── ORDER ────────────────────────────────────────────────────────────────────

func handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !basicAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Receipt  string `json:"receipt"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	orderID := fmt.Sprintf("order_%s", uuid.New().String()[:14])

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         orderID,
		"amount":     req.Amount,
		"currency":   req.Currency,
		"receipt":    req.Receipt,
		"status":     "created",
		"created_at": time.Now().Unix(),
	})

	// No webhook on order creation — Razorpay only webhooks on payment events
}

// ── PAYMENTS ─────────────────────────────────────────────────────────────────

func handlePayments(w http.ResponseWriter, r *http.Request) {
	if !basicAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/payments/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	paymentID := parts[0]
	action := parts[1]

	switch action {
	case "capture":
		handleCapture(w, r, paymentID)
	case "refund":
		handleRefund(w, r, paymentID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown action"})
	}
}

// ── CAPTURE ──────────────────────────────────────────────────────────────────

func handleCapture(w http.ResponseWriter, r *http.Request, paymentID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Amount  int64  `json:"amount"`
		OrderID string `json:"order_id"` // payment-gateway sends this so we can webhook back
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Send capture response to payment-gateway
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       paymentID,
		"amount":   req.Amount,
		"status":   "captured",
		"method":   "upi",
		"captured": true,
	})

	// Automatically fire payment.captured webhook after a short delay
	// Delay mimics Razorpay's async webhook delivery (usually 1-3s after capture)
	go func() {
		time.Sleep(2 * time.Second)
		fireWebhook("payment.captured", req.OrderID, paymentID, "", req.Amount)
	}()
}

// ── REFUND ───────────────────────────────────────────────────────────────────

func handleRefund(w http.ResponseWriter, r *http.Request, paymentID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Amount int64  `json:"amount"`
		Notes  string `json:"notes"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	refundID := fmt.Sprintf("rfnd_%s", uuid.New().String()[:14])

	// Send refund response to payment-gateway
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         refundID,
		"payment_id": paymentID,
		"amount":     req.Amount,
		"status":     "processed",
	})

	// Automatically fire refund.processed webhook after a short delay
	go func() {
		time.Sleep(2 * time.Second)
		fireWebhook("refund.processed", "", paymentID, refundID, req.Amount)
	}()
}

// ── SIGNATURE HELPER ─────────────────────────────────────────────────────────

func handleSign(w http.ResponseWriter, r *http.Request) {
	orderID := r.URL.Query().Get("order_id")
	paymentID := r.URL.Query().Get("payment_id")
	secret := r.URL.Query().Get("secret")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%s|%s", orderID, paymentID)))

	writeJSON(w, http.StatusOK, map[string]string{
		"signature": hex.EncodeToString(mac.Sum(nil)),
	})
}

// ── WEBHOOK FIRE ─────────────────────────────────────────────────────────────

// fireWebhook builds and sends a Razorpay-shaped webhook to the payment-gateway.
// Called automatically after capture and refund — no manual trigger needed.
func fireWebhook(event, orderID, paymentID, refundID string, amount int64) {
	eventID := fmt.Sprintf("evt_%s", uuid.New().String()[:14])

	var payload map[string]interface{}

	switch event {
	case "payment.captured":
		payload = map[string]interface{}{
			"entity":     "event",
			"account_id": "mock_account",
			"event":      event,
			"payload": map[string]interface{}{
				"payment": map[string]interface{}{
					"entity": map[string]interface{}{
						"id":       paymentID,
						"order_id": orderID,
						"status":   "captured",
						"method":   "upi",
						"amount":   amount,
					},
				},
			},
		}

	case "refund.processed":
		payload = map[string]interface{}{
			"entity":     "event",
			"account_id": "mock_account",
			"event":      event,
			"payload": map[string]interface{}{
				"refund": map[string]interface{}{
					"entity": map[string]interface{}{
						"id":         refundID,
						"payment_id": paymentID,
						"amount":     amount,
						"status":     "processed",
					},
				},
			},
		}

	default:
		log.Printf("[mock-razorpay] unknown event type: %s", event)
		return
	}

	rawBody, _ := json.Marshal(payload)

	// Sign with webhook secret
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(rawBody)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, webhookTarget, bytes.NewBuffer(rawBody))
	if err != nil {
		log.Printf("[mock-razorpay] webhook build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Razorpay-Signature", sig)
	req.Header.Set("X-Razorpay-Event-Id", eventID)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[mock-razorpay] webhook fire failed: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Printf("[mock-razorpay] webhook fired event=%s status=%d", event, resp.StatusCode)
}
