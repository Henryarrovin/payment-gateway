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

func InitializeContainer(cfgFile string, logger *zap.Logger) (*handlers.PaymentHandler, func(), error) {
	wire.Build(
		config.ProviderSet,
		data.ProviderSet,
		provideRazorpayAdapter,
		services.ProviderSet,
		handlers.ProviderSet,
	)
	return nil, nil, nil
}

func provideRazorpayAdapter(logger *zap.Logger) razorpay.Adapter {
	return razorpay.NewAdapter(logger)
}
