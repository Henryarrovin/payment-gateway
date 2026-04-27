//go:build wireinject

package wire

import (
	"payment-gateway/config"
	"payment-gateway/data"
	"payment-gateway/external/razorpay"
	"payment-gateway/handlers"
	"payment-gateway/services"

	"github.com/google/wire"
	"go.uber.org/zap"
)

type Container struct {
	PaymentHandler *handlers.PaymentHandler
	WebhookHandler *handlers.WebhookHandler
}

func InitializeContainer(cfgFile string, logger *zap.Logger) (*Container, func(), error) {
	wire.Build(
		config.ProviderSet,
		data.ProviderSet,
		provideRazorpayAdapter,
		services.ProviderSet,
		handlers.ProviderSet,
		wire.Struct(new(Container), "*"),
	)
	return nil, nil, nil
}

func provideRazorpayAdapter(logger *zap.Logger) razorpay.Adapter {
	return razorpay.NewAdapter(logger)
}
