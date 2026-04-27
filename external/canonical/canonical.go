// Package canonical generates the HMAC-SHA256 canonical request signature
// used when calling the auth-service ValidateToken gRPC endpoint.
// This must match the logic in auth-service/services/jwt_service/jwt_service.go.
package canonical

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// DateFormat is the date format used in the canonical string.
const DateFormat = "20060102T150405Z"

// Now returns the current UTC time in the canonical date format.
func Now() string {
	return time.Now().UTC().Format(DateFormat)
}

// BuildString assembles the canonical string-to-sign.
// Canonical string = METHOD\nPATH\nDATE\nSERVICE
func BuildString(method, path, date, service string) string {
	return strings.Join([]string{
		strings.ToUpper(method),
		path,
		date,
		service,
	}, "\n")
}

// Sign produces the HMAC-SHA256 hex signature for a canonical request.
func Sign(method, path, date, service, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(BuildString(method, path, date, service)))
	return hex.EncodeToString(mac.Sum(nil))
}
