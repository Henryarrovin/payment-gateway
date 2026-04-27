package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"payment-gateway/config"
	"payment-gateway/external/canonical"

	authpb "github.com/Henryarrovin/auth-service/proto/authpb"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	CorrelationIDKey = "x-correlation-id"
	UserIDKey        = "x-user-id"
	UserRolesKey     = "x-user-roles"
	UserEmailKey     = "x-user-email"
)

type AuthClaims struct {
	UserID string
	Email  string
	Roles  []string
}

type authClaimsKey struct{}

// InjectClaims stores validated claims into the context.
func InjectClaims(ctx context.Context, claims *AuthClaims) context.Context {
	return context.WithValue(ctx, authClaimsKey{}, claims)
}

// ClaimsFromContext retrieves validated claims from context.
func ClaimsFromContext(ctx context.Context) (*AuthClaims, bool) {
	c, ok := ctx.Value(authClaimsKey{}).(*AuthClaims)
	return c, ok
}

type bodyLogWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyLogWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func HTTPLogger(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			correlationID := r.Header.Get(CorrelationIDKey)
			if correlationID == "" {
				correlationID = uuid.NewString()
			}
			w.Header().Set(CorrelationIDKey, correlationID)

			var bodyBytes []byte
			if r.Body != nil {
				bodyBytes, _ = io.ReadAll(r.Body)
			}
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			blw := &bodyLogWriter{
				ResponseWriter: w,
				body:           bytes.NewBufferString(""),
				statusCode:     http.StatusOK,
			}

			enriched := logger.With(
				zap.String("correlation_id", correlationID),
				zap.String("transport", "http"),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("remote_addr", r.RemoteAddr),
			)
			ctx := InjectLogger(r.Context(), enriched)
			ctx = context.WithValue(ctx, CorrelationIDKey, correlationID)
			r = r.WithContext(ctx)

			enriched.Info("http request")
			next.ServeHTTP(blw, r)
			enriched.Info("http response",
				zap.Int("status", blw.statusCode),
				zap.Duration("latency", time.Since(start)),
			)
		})
	}
}

func UnaryRecovery(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())),
				)
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

func UnaryLogger(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		correlationID := extractGRPCCorrelationID(ctx)
		if correlationID == "" {
			correlationID = uuid.NewString()
		}

		enriched := logger.With(
			zap.String("correlation_id", correlationID),
			zap.String("transport", "grpc"),
			zap.String("method", info.FullMethod),
		)
		ctx = InjectLogger(ctx, enriched)
		ctx = context.WithValue(ctx, CorrelationIDKey, correlationID)

		enriched.Info("grpc request", zap.Any("request", req))

		resp, err := handler(ctx, req)

		code := codes.OK
		if s, ok := status.FromError(err); ok {
			code = s.Code()
		}

		enriched.Info("grpc response",
			zap.String("code", code.String()),
			zap.Duration("latency", time.Since(start)),
			zap.Error(err),
		)
		return resp, err
	}
}

// UnaryAuth validates the Bearer token by calling auth-service via gRPC.
// It injects AuthClaims into the context so handlers can access user identity.
func UnaryAuth(authClient authpb.AuthServiceClient, cfg config.AuthGRPCConfig, logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log := FromContext(ctx, logger)

		token, err := extractBearerToken(ctx)
		if err != nil {
			log.Warn("auth.missing_token", zap.String("method", info.FullMethod))
			return nil, status.Error(codes.Unauthenticated, "missing or invalid Authorization header")
		}

		date := canonical.Now()
		parts := strings.Split(info.FullMethod, "/")
		methodName := parts[len(parts)-1]
		sig := canonical.Sign("POST", "/api/v1/auth/validate", date, cfg.ServiceName, cfg.CanonicalSecret)

		validateCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()

		resp, err := authClient.ValidateToken(validateCtx, &authpb.ValidateTokenRequest{
			Token:           token,
			CanonicalMethod: "POST",
			CanonicalPath:   "/api/v1/auth/validate",
			CanonicalDate:   date,
			ServiceName:     cfg.ServiceName,
			CanonicalSig:    sig,
		})
		if err != nil {
			log.Error("auth.grpc_call_failed", zap.Error(err))
			return nil, status.Error(codes.Internal, "auth service unavailable")
		}

		if !resp.Valid {
			log.Warn("auth.token_invalid",
				zap.String("method", methodName),
				zap.String("error", resp.Error),
			)
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}

		claims := &AuthClaims{
			UserID: resp.UserId,
			Email:  resp.Email,
			Roles:  resp.Roles,
		}
		ctx = InjectClaims(ctx, claims)

		log.Info("auth.token_validated",
			zap.String("user_id", claims.UserID),
			zap.Strings("roles", claims.Roles),
			zap.String("method", methodName),
		)

		return handler(ctx, req)
	}
}

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no metadata")
	}

	// gRPC-gateway forwards the HTTP Authorization header as "authorization"
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", status.Error(codes.Unauthenticated, "no authorization header")
	}

	parts := strings.SplitN(vals[0], " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", status.Error(codes.Unauthenticated, "invalid authorization format")
	}
	return parts[1], nil
}

func extractGRPCCorrelationID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get(CorrelationIDKey)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
