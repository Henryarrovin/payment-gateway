package services

import (
	"context"
	"fmt"
	"payment-gateway/data"
	"payment-gateway/external/razorpay"
	"payment-gateway/middleware"
	"payment-gateway/models"

	"go.uber.org/zap"
)

type PaymentService struct {
	orders      *data.OrderRepository
	payments    *data.PaymentRepository
	permissions *data.PermissionRepository
	providers   *data.ProviderRepository
	adapter     razorpay.Adapter
	logger      *zap.Logger
}

func NewPaymentService(
	orders *data.OrderRepository,
	payments *data.PaymentRepository,
	permissions *data.PermissionRepository,
	providers *data.ProviderRepository,
	adapter razorpay.Adapter,
	logger *zap.Logger,
) *PaymentService {
	return &PaymentService{
		orders:      orders,
		payments:    payments,
		permissions: permissions,
		providers:   providers,
		adapter:     adapter,
		logger:      logger,
	}
}

// authorize checks DB-driven permissions. Returns error if the user's roles
// are not allowed for the given endpoint. Endpoint names match gRPC method names.
func (s *PaymentService) authorize(ctx context.Context, endpoint string, claims *middleware.AuthClaims) error {
	log := middleware.FromContext(ctx, s.logger)

	allowed, err := s.permissions.IsAllowed(ctx, endpoint, claims.Roles)
	if err != nil {
		log.Error("permission.fetch_failed",
			zap.String("endpoint", endpoint),
			zap.Error(err),
		)
		return fmt.Errorf("permission check failed: %w", err)
	}
	if !allowed {
		log.Warn("permission.denied",
			zap.String("endpoint", endpoint),
			zap.String("user_id", claims.UserID),
			zap.Strings("roles", claims.Roles),
		)
		return ErrForbidden
	}
	return nil
}

type CreateOrderInput struct {
	Amount   int64
	Currency string
	Notes    string
}

type CreateOrderOutput struct {
	OrderID         string
	ProviderOrderID string
	Amount          int64
	Currency        string
	Status          string
	KeyID           string // passed to frontend for Razorpay checkout
}

func (s *PaymentService) CreateOrder(ctx context.Context, claims *middleware.AuthClaims, in CreateOrderInput) (*CreateOrderOutput, error) {
	log := middleware.FromContext(ctx, s.logger)

	if err := s.authorize(ctx, "CreateOrder", claims); err != nil {
		return nil, err
	}

	provider, err := s.providers.GetActiveProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching provider: %w", err)
	}

	log.Info("payment_service.create_order",
		zap.String("user_id", claims.UserID),
		zap.Int64("amount", in.Amount),
		zap.String("provider", provider.Name),
		zap.Bool("mock", provider.IsMock),
	)

	result, err := s.adapter.CreateOrder(ctx, provider, razorpay.CreateOrderInput{
		Amount:   in.Amount,
		Currency: in.Currency,
		Notes:    in.Notes,
	})
	if err != nil {
		return nil, fmt.Errorf("provider.create_order: %w", err)
	}

	order := &models.Order{
		UserID:          claims.UserID,
		ProviderID:      provider.ID,
		ProviderOrderID: result.ProviderOrderID,
		Amount:          in.Amount,
		Currency:        in.Currency,
		Status:          models.OrderStatusPending,
		Notes:           in.Notes,
	}
	if err := s.orders.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("persisting order: %w", err)
	}

	payment := &models.Payment{
		OrderID: order.ID,
		Status:  models.OrderStatusPending,
	}
	if err := s.payments.Create(ctx, payment); err != nil {
		return nil, fmt.Errorf("persisting payment: %w", err)
	}

	log.Info("payment_service.order_created",
		zap.String("order_id", order.ID),
		zap.String("provider_order_id", result.ProviderOrderID),
	)

	return &CreateOrderOutput{
		OrderID:         order.ID,
		ProviderOrderID: result.ProviderOrderID,
		Amount:          result.Amount,
		Currency:        result.Currency,
		Status:          result.Status,
		KeyID:           provider.KeyID,
	}, nil
}

type CaptureInput struct {
	OrderID           string
	ProviderPaymentID string
	ProviderSignature string
	Method            string
}

type CaptureOutput struct {
	PaymentID string
	Status    string
	Message   string
}

func (s *PaymentService) CapturePayment(ctx context.Context, claims *middleware.AuthClaims, in CaptureInput) (*CaptureOutput, error) {
	log := middleware.FromContext(ctx, s.logger)

	if err := s.authorize(ctx, "CapturePayment", claims); err != nil {
		log.Error("payment_service.authorization_failed",
			zap.String("user_id", claims.UserID),
			zap.Strings("roles", claims.Roles),
			zap.String("endpoint", "CapturePayment"),
		)
		return nil, err
	}

	order, err := s.orders.FindByID(ctx, in.OrderID)
	if err != nil {
		log.Error("payment_service.order_not_found",
			zap.String("order_id", in.OrderID),
		)
		return nil, fmt.Errorf("order not found: %w", err)
	}

	if !isAdmin(claims.Roles) && order.UserID != claims.UserID {
		log.Warn("payment_service.order_access_denied",
			zap.String("user_id", claims.UserID),
			zap.String("order_id", order.ID),
		)
		return nil, ErrForbidden
	}

	provider := &order.Provider
	payment, err := s.payments.FindByOrderID(ctx, order.ID)
	if err != nil {
		log.Error("payment_service.payment_not_found",
			zap.String("order_id", order.ID),
		)
		return nil, fmt.Errorf("payment record not found: %w", err)
	}

	log.Info("payment_service.capture_payment",
		zap.String("order_id", order.ID),
		zap.String("provider_payment_id", in.ProviderPaymentID),
	)

	result, err := s.adapter.CapturePayment(ctx, provider, razorpay.CaptureInput{
		ProviderOrderID:   order.ProviderOrderID,
		ProviderPaymentID: in.ProviderPaymentID,
		ProviderSignature: in.ProviderSignature,
		Amount:            order.Amount,
		KeySecret:         provider.KeySecret,
	})
	if err != nil {
		log.Error("payment_service.capture_failed",
			zap.String("order_id", order.ID),
			zap.String("provider_payment_id", in.ProviderPaymentID),
			zap.Error(err),
		)
		_ = s.payments.Fail(ctx, payment.ID, err.Error())
		_ = s.orders.UpdateStatus(ctx, order.ID, models.OrderStatusFailed)
		return nil, fmt.Errorf("capture failed: %w", err)
	}

	if err := s.payments.Capture(ctx, payment.ID, result.ProviderPaymentID, result.Method); err != nil {
		log.Error("payment_service.update_payment_failed",
			zap.String("payment_id", payment.ID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("updating payment: %w", err)
	}
	if err := s.orders.UpdateStatus(ctx, order.ID, models.OrderStatusPaid); err != nil {
		log.Error("payment_service.update_order_failed",
			zap.String("order_id", order.ID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("updating order: %w", err)
	}

	log.Info("payment_service.payment_captured",
		zap.String("payment_id", payment.ID),
		zap.String("provider_payment_id", result.ProviderPaymentID),
	)

	return &CaptureOutput{
		PaymentID: payment.ID,
		Status:    "paid",
		Message:   "payment captured successfully",
	}, nil
}

func (s *PaymentService) GetOrder(ctx context.Context, claims *middleware.AuthClaims, orderID string) (*models.Order, error) {
	if err := s.authorize(ctx, "GetOrder", claims); err != nil {
		return nil, err
	}

	order, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}

	if !isAdmin(claims.Roles) && order.UserID != claims.UserID {
		return nil, ErrForbidden
	}
	return order, nil
}

func (s *PaymentService) ListOrders(ctx context.Context, claims *middleware.AuthClaims) ([]models.Order, error) {
	if err := s.authorize(ctx, "ListOrders", claims); err != nil {
		return nil, err
	}
	return s.orders.ListByUserID(ctx, claims.UserID)
}

func (s *PaymentService) GetPayment(ctx context.Context, claims *middleware.AuthClaims, paymentID string) (*models.Payment, error) {
	if err := s.authorize(ctx, "GetPayment", claims); err != nil {
		return nil, err
	}

	payment, err := s.payments.FindByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	if !isAdmin(claims.Roles) && payment.Order.UserID != claims.UserID {
		return nil, ErrForbidden
	}
	return payment, nil
}

type RefundInput struct {
	PaymentID string
	Amount    int64
	Notes     string
}

type RefundOutput struct {
	RefundID         string
	ProviderRefundID string
	Status           string
	Message          string
}

func (s *PaymentService) RefundPayment(ctx context.Context, claims *middleware.AuthClaims, in RefundInput) (*RefundOutput, error) {
	log := middleware.FromContext(ctx, s.logger)

	if err := s.authorize(ctx, "RefundPayment", claims); err != nil {
		return nil, err
	}

	payment, err := s.payments.FindByID(ctx, in.PaymentID)
	if err != nil {
		return nil, fmt.Errorf("payment not found: %w", err)
	}

	if payment.Status != models.OrderStatusPaid {
		return nil, fmt.Errorf("payment is not in captured state")
	}

	order, err := s.orders.FindByID(ctx, payment.OrderID)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	provider := &order.Provider

	refundAmount := in.Amount
	if refundAmount == 0 {
		refundAmount = payment.Order.Amount
	}

	log.Info("payment_service.refund_payment",
		zap.String("payment_id", payment.ID),
		zap.Int64("amount", refundAmount),
	)

	result, err := s.adapter.Refund(ctx, provider, razorpay.RefundInput{
		ProviderPaymentID: payment.ProviderPaymentID,
		Amount:            refundAmount,
		Notes:             in.Notes,
	})
	if err != nil {
		return nil, fmt.Errorf("refund failed: %w", err)
	}

	refund := &models.Refund{
		PaymentID:        payment.ID,
		ProviderRefundID: result.ProviderRefundID,
		Amount:           refundAmount,
		Status:           result.Status,
		Notes:            in.Notes,
	}
	if err := s.payments.CreateRefund(ctx, refund); err != nil {
		return nil, fmt.Errorf("persisting refund: %w", err)
	}

	if err := s.orders.UpdateStatus(ctx, order.ID, models.OrderStatusRefunded); err != nil {
		return nil, fmt.Errorf("updating order status: %w", err)
	}

	log.Info("payment_service.refund_processed",
		zap.String("refund_id", refund.ID),
		zap.String("provider_refund_id", result.ProviderRefundID),
	)

	return &RefundOutput{
		RefundID:         refund.ID,
		ProviderRefundID: result.ProviderRefundID,
		Status:           result.Status,
		Message:          "refund processed successfully",
	}, nil
}

func isAdmin(roles []string) bool {
	for _, r := range roles {
		if r == "admin" {
			return true
		}
	}
	return false
}
