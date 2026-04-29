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
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	webhookTarget string
	webhookSecret string
	listenAddr    string
)

func init() {
	webhookTarget = getEnv("MOCK_WEBHOOK_TARGET", "http://localhost:8081/api/v1/payments/webhook")
	webhookSecret = getEnv("MOCK_WEBHOOK_SECRET", "rzp_webhook_mock_secret")
	listenAddr = getEnv("MOCK_LISTEN_ADDR", ":8090")
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func main() {
	log.Printf("mock-razorpay starting")
	log.Printf("  listen_addr    = %s", listenAddr)
	log.Printf("  webhook_target = %s", webhookTarget)
	log.Printf("  webhook_secret = %s", webhookSecret)

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/orders", handleCreateOrder)
	mux.HandleFunc("/v1/payments/", handlePayments)
	mux.HandleFunc("/v1/sign", handleSign)

	log.Printf("mock-razorpay listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, loggingMiddleware(mux)); err != nil {
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

// ── ORDER ─────────────────────────────────────────────────────────────────────

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
}

// ── PAYMENTS ──────────────────────────────────────────────────────────────────

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

// ── CAPTURE ───────────────────────────────────────────────────────────────────

func handleCapture(w http.ResponseWriter, r *http.Request, paymentID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Amount  int64  `json:"amount"`
		OrderID string `json:"order_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       paymentID,
		"amount":   req.Amount,
		"status":   "captured",
		"method":   "upi",
		"captured": true,
	})

	go func() {
		time.Sleep(2 * time.Second)
		fireWebhook("payment.captured", req.OrderID, paymentID, "", req.Amount)
	}()
}

// ── REFUND ────────────────────────────────────────────────────────────────────

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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         refundID,
		"payment_id": paymentID,
		"amount":     req.Amount,
		"status":     "processed",
	})

	go func() {
		time.Sleep(2 * time.Second)
		fireWebhook("refund.processed", "", paymentID, refundID, req.Amount)
	}()
}

// ── SIGNATURE HELPER ──────────────────────────────────────────────────────────

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

// ── WEBHOOK FIRE ──────────────────────────────────────────────────────────────

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

	log.Printf("[mock-razorpay] webhook fired event=%s target=%s status=%d", event, webhookTarget, resp.StatusCode)
}
