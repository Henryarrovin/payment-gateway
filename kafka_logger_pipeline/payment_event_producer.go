package kafka_logger_pipeline

import (
	"encoding/json"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
)

type PaymentEventProducer struct {
	producer sarama.SyncProducer
	topic    string
	logger   *zap.Logger
}

func NewPaymentEventProducer(brokers []string, topic string, logger *zap.Logger) (*PaymentEventProducer, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 3

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, err
	}
	return &PaymentEventProducer{producer: producer, topic: topic, logger: logger}, nil
}

func (p *PaymentEventProducer) PublishPaymentCaptured(paymentID, orderID, userID string, amount int64) error {
	event := map[string]any{
		"event": "payment.captured",
		"payload": map[string]any{
			"payment": map[string]any{
				"entity": map[string]any{
					"id":       paymentID,
					"order_id": orderID,
					"amount":   amount,
					"user_id":  userID,
				},
			},
		},
	}
	return p.publish(paymentID, event)
}

func (p *PaymentEventProducer) PublishRefundProcessed(refundID, paymentID, userID string, amount int64) error {
	event := map[string]any{
		"event": "refund.processed",
		"payload": map[string]any{
			"refund": map[string]any{
				"entity": map[string]any{
					"id":         refundID,
					"payment_id": paymentID,
					"amount":     amount,
					"user_id":    userID,
				},
			},
		},
	}
	return p.publish(refundID, event)
}

func (p *PaymentEventProducer) publish(key string, event any) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.StringEncoder(body),
	}
	_, _, err = p.producer.SendMessage(msg)
	if err != nil {
		p.logger.Error("payment_event_producer.send_failed", zap.Error(err))
	}
	return err
}

func (p *PaymentEventProducer) Close() error {
	return p.producer.Close()
}
