package kafka_logger_pipeline

import (
	"payment-gateway/config"

	"github.com/google/wire"
	"go.uber.org/zap"
)

var ProviderSet = wire.NewSet(NewPaymentEventProducerProvider)

func NewPaymentEventProducerProvider(cfg config.KafkaConfig, logger *zap.Logger) (*PaymentEventProducer, func(), error) {
	p, err := NewPaymentEventProducer(cfg.Brokers, cfg.PaymentEventsTopic, logger)
	if err != nil {
		return nil, nil, err
	}
	return p, func() { p.Close() }, nil
}
