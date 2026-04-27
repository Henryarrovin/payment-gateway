package kafka_logger_pipeline

import (
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type KafkaCore struct {
	producer sarama.SyncProducer
	topic    string
	level    zapcore.Level
	enc      zapcore.Encoder
}

func NewKafkaCore(brokers []string, topic string, level zapcore.Level) (*KafkaCore, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 3

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, err
	}

	encCfg := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	return &KafkaCore{
		producer: producer,
		topic:    topic,
		level:    level,
		enc:      zapcore.NewJSONEncoder(encCfg),
	}, nil
}

func (k *KafkaCore) Enabled(level zapcore.Level) bool {
	return level >= k.level
}

func (k *KafkaCore) With(fields []zapcore.Field) zapcore.Core {
	clone := *k
	enc := k.enc.Clone()
	for _, f := range fields {
		f.AddTo(enc)
	}
	clone.enc = enc
	return &clone
}

func (k *KafkaCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if k.Enabled(entry.Level) {
		return checked.AddCore(entry, k)
	}
	return checked
}

func (k *KafkaCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	buf, err := k.enc.EncodeEntry(entry, fields)
	if err != nil {
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: k.topic,
		Key:   sarama.StringEncoder(time.Now().UTC().Format("2006-01-02")),
		Value: sarama.StringEncoder(buf.String()),
	}

	_, _, err = k.producer.SendMessage(msg)
	return err
}

func (k *KafkaCore) Sync() error {
	return k.producer.Close()
}

func NewKafkaCoreWithLogger(cfg KafkaCoreCfg) (*KafkaCore, error) {
	return NewKafkaCore(cfg.Brokers, cfg.Topic, zapcore.InfoLevel)
}

type KafkaCoreCfg struct {
	Brokers []string
	Topic   string
}

// ZapField helper — attaches kafka fields to logger
func Fields(logger *zap.Logger) *zap.Logger {
	return logger.With(zap.String("service", "payment-gateway"))
}
