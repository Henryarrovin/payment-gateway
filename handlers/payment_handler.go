package handlers

import (
	"context"
	"errors"
	"payment-gateway/middleware"
	paymentpb "payment-gateway/proto/paymentpb"
	"payment-gateway/services"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PaymentHandler struct {
	paymentpb.UnimplementedPaymentServiceServer
	svc    *services.PaymentService
	logger *zap.Logger
}

func NewPaymentHandler(svc *services.PaymentService, logger *zap.Logger) *PaymentHandler {
	return &PaymentHandler{svc: svc, logger: logger}
}

func (h *PaymentHandler) claims(ctx context.Context) (*middleware.AuthClaims, error) {
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing auth claims")
	}
	return claims, nil
}

func mapServiceError(err error) error {
	switch {
	case errors.Is(err, services.ErrForbidden):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, services.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, services.ErrBadRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Errorf(codes.Internal, "internal error: %v", err)
	}
}

func (h *PaymentHandler) CreateOrder(ctx context.Context, req *paymentpb.CreateOrderRequest) (*paymentpb.CreateOrderResponse, error) {
	log := middleware.FromContext(ctx, h.logger)

	if req.Amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be greater than 0")
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}

	claims, err := h.claims(ctx)
	if err != nil {
		return nil, err
	}

	out, err := h.svc.CreateOrder(ctx, claims, services.CreateOrderInput{
		Amount:   req.Amount,
		Currency: req.Currency,
		Notes:    req.Notes,
	})
	if err != nil {
		log.Error("handler.create_order.failed", zap.Error(err))
		return nil, mapServiceError(err)
	}

	return &paymentpb.CreateOrderResponse{
		OrderId:         out.OrderID,
		ProviderOrderId: out.ProviderOrderID,
		Amount:          out.Amount,
		Currency:        out.Currency,
		Status:          out.Status,
		KeyId:           out.KeyID,
	}, nil
}

func (h *PaymentHandler) CapturePayment(ctx context.Context, req *paymentpb.CapturePaymentRequest) (*paymentpb.CapturePaymentResponse, error) {
	log := middleware.FromContext(ctx, h.logger)

	if req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}
	if req.ProviderPaymentId == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_payment_id is required")
	}

	claims, err := h.claims(ctx)
	if err != nil {
		return nil, err
	}

	out, err := h.svc.CapturePayment(ctx, claims, services.CaptureInput{
		OrderID:           req.OrderId,
		ProviderPaymentID: req.ProviderPaymentId,
		ProviderSignature: req.ProviderSignature,
		Method:            req.Method,
	})
	if err != nil {
		log.Error("handler.capture_payment.failed", zap.Error(err))
		return nil, mapServiceError(err)
	}

	return &paymentpb.CapturePaymentResponse{
		PaymentId: out.PaymentID,
		Status:    out.Status,
		Message:   out.Message,
	}, nil
}

func (h *PaymentHandler) GetOrder(ctx context.Context, req *paymentpb.GetOrderRequest) (*paymentpb.GetOrderResponse, error) {
	log := middleware.FromContext(ctx, h.logger)

	if req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}

	claims, err := h.claims(ctx)
	if err != nil {
		return nil, err
	}

	order, err := h.svc.GetOrder(ctx, claims, req.OrderId)
	if err != nil {
		log.Error("handler.get_order.failed", zap.String("order_id", req.OrderId), zap.Error(err))
		return nil, mapServiceError(err)
	}

	return &paymentpb.GetOrderResponse{
		OrderId:         order.ID,
		ProviderOrderId: order.ProviderOrderID,
		Amount:          order.Amount,
		Currency:        order.Currency,
		Status:          string(order.Status),
		UserId:          order.UserID,
		CreatedAt:       order.CreatedAt.String(),
	}, nil
}

func (h *PaymentHandler) ListOrders(ctx context.Context, req *paymentpb.ListOrdersRequest) (*paymentpb.ListOrdersResponse, error) {
	log := middleware.FromContext(ctx, h.logger)

	claims, err := h.claims(ctx)
	if err != nil {
		return nil, err
	}

	orders, err := h.svc.ListOrders(ctx, claims)
	if err != nil {
		log.Error("handler.list_orders.failed", zap.Error(err))
		return nil, mapServiceError(err)
	}

	resp := &paymentpb.ListOrdersResponse{
		Orders: make([]*paymentpb.GetOrderResponse, len(orders)),
	}
	for i, o := range orders {
		resp.Orders[i] = &paymentpb.GetOrderResponse{
			OrderId:         o.ID,
			ProviderOrderId: o.ProviderOrderID,
			Amount:          o.Amount,
			Currency:        o.Currency,
			Status:          string(o.Status),
			UserId:          o.UserID,
			CreatedAt:       o.CreatedAt.String(),
		}
	}
	return resp, nil
}

func (h *PaymentHandler) GetPayment(ctx context.Context, req *paymentpb.GetPaymentRequest) (*paymentpb.GetPaymentResponse, error) {
	log := middleware.FromContext(ctx, h.logger)

	if req.PaymentId == "" {
		return nil, status.Error(codes.InvalidArgument, "payment_id is required")
	}

	claims, err := h.claims(ctx)
	if err != nil {
		return nil, err
	}

	payment, err := h.svc.GetPayment(ctx, claims, req.PaymentId)
	if err != nil {
		log.Error("handler.get_payment.failed", zap.String("payment_id", req.PaymentId), zap.Error(err))
		return nil, mapServiceError(err)
	}

	capturedAt := ""
	if payment.CapturedAt != nil {
		capturedAt = payment.CapturedAt.String()
	}

	return &paymentpb.GetPaymentResponse{
		PaymentId:         payment.ID,
		OrderId:           payment.OrderID,
		ProviderPaymentId: payment.ProviderPaymentID,
		Method:            payment.Method,
		Status:            string(payment.Status),
		FailureReason:     payment.FailureReason,
		CapturedAt:        capturedAt,
	}, nil
}

func (h *PaymentHandler) RefundPayment(ctx context.Context, req *paymentpb.RefundPaymentRequest) (*paymentpb.RefundPaymentResponse, error) {
	log := middleware.FromContext(ctx, h.logger)

	if req.PaymentId == "" {
		return nil, status.Error(codes.InvalidArgument, "payment_id is required")
	}

	claims, err := h.claims(ctx)
	if err != nil {
		return nil, err
	}

	out, err := h.svc.RefundPayment(ctx, claims, services.RefundInput{
		PaymentID: req.PaymentId,
		Amount:    req.Amount,
		Notes:     req.Notes,
	})
	if err != nil {
		log.Error("handler.refund_payment.failed", zap.String("payment_id", req.PaymentId), zap.Error(err))
		return nil, mapServiceError(err)
	}

	return &paymentpb.RefundPaymentResponse{
		RefundId:         out.RefundID,
		ProviderRefundId: out.ProviderRefundID,
		Status:           out.Status,
		Message:          out.Message,
	}, nil
}
