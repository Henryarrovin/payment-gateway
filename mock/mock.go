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

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/orders", handleCreateOrder)
	mux.HandleFunc("/v1/payments/", handlePayments)
	mux.HandleFunc("/v1/sign", handleSign)
	mux.HandleFunc("/v1/webhook/fire", handleFireWebhook) // ✅ added

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

//
// ── ORDER ───────────────────────────────────────────────────────────
//

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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         fmt.Sprintf("order_%s", uuid.New().String()[:14]),
		"amount":     req.Amount,
		"currency":   req.Currency,
		"receipt":    req.Receipt,
		"status":     "created",
		"created_at": time.Now().Unix(),
	})
}

//
// ── PAYMENTS ────────────────────────────────────────────────────────
//

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

func handleCapture(w http.ResponseWriter, r *http.Request, paymentID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Amount int64 `json:"amount"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       paymentID,
		"amount":   req.Amount,
		"status":   "captured",
		"method":   "upi",
		"captured": true,
	})
}

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

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         fmt.Sprintf("rfnd_%s", uuid.New().String()[:14]),
		"payment_id": paymentID,
		"amount":     req.Amount,
		"status":     "processed",
	})
}

//
// ── SIGNATURE HELPER ────────────────────────────────────────────────
//

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

//
// ── WEBHOOK TRIGGER ─────────────────────────────────────────────────
//

func handleFireWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	target := r.URL.Query().Get("target")
	if target == "" {
		target = "http://localhost:8081/api/v1/payments/webhook"
	}

	var req struct {
		Event     string `json:"event"`
		OrderID   string `json:"order_id"`
		PaymentID string `json:"payment_id"`
		RefundID  string `json:"refund_id"`
		Amount    int64  `json:"amount"`
		Secret    string `json:"secret"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	eventID := fmt.Sprintf("evt_%s", uuid.New().String()[:14])

	payload := map[string]interface{}{
		"event": req.Event,
		"payload": map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":       req.PaymentID,
					"order_id": req.OrderID,
					"status":   strings.Replace(req.Event, "payment.", "", 1),
					"amount":   req.Amount,
				},
			},
		},
	}

	rawBody, _ := json.Marshal(payload)

	mac := hmac.New(sha256.New, []byte(req.Secret))
	mac.Write(rawBody)
	sig := hex.EncodeToString(mac.Sum(nil))

	httpReq, _ := http.NewRequest(http.MethodPost, target, bytes.NewBuffer(rawBody))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Razorpay-Signature", sig)
	httpReq.Header.Set("X-Razorpay-Event-Id", eventID)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"event":  req.Event,
		"status": resp.StatusCode,
	})
}
