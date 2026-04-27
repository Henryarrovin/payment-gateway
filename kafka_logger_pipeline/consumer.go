package kafka_logger_pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
)

const (
	// Logs land at:
	//   Windows : C:\Users\<user>\Desktop\logs\payment-gateway\log-YYYY-MM-DD.log
	//   Linux   : ~/Desktop/logs/payment-gateway/log-YYYY-MM-DD.log  (dev)
	//   Server  : /apps/logs/payment-gateway/log-YYYY-MM-DD.log      (k8s PVC)
	serviceName    = "payment-gateway"
	dockerLogBase  = "/apps/logs"
	dockerFlagFile = "/.dockerenv"
)

type LogConsumer struct {
	brokers []string
	topic   string
	groupID string
	logDir  string
	logger  *zap.Logger
	files   map[string]*os.File
	mu      sync.Mutex
}

func NewLogConsumer(brokers []string, topic, groupID, logDir string, logger *zap.Logger) *LogConsumer {
	resolvedDir := resolveLogDir(serviceName, logger)
	return &LogConsumer{
		brokers: brokers,
		topic:   topic,
		groupID: groupID,
		logDir:  resolvedDir,
		logger:  logger,
		files:   make(map[string]*os.File),
	}
}

// Server / Docker (k8s PVC):  /apps/logs/payment-gateway/
// Linux/macOS dev:            ~/Desktop/logs/payment-gateway/
// Windows dev:                C:\Users\<user>\Desktop\logs\payment-gateway\
func resolveLogDir(service string, logger *zap.Logger) string {
	if isDocker() {
		dir := filepath.Join(dockerLogBase, service)
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.Warn("server: could not create log dir, falling back to cwd",
				zap.String("path", dir),
				zap.Error(err),
			)
			return ensureDir(filepath.Join(".", "logs", service), logger)
		}
		logger.Info("server environment detected",
			zap.String("log_dir", dir),
		)
		return dir
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Warn("could not get home dir, using ./logs", zap.Error(err))
		return ensureDir(filepath.Join(".", "logs", service), logger)
	}

	desktopDir := filepath.Join(homeDir, "Desktop", "logs", service)

	if runtime.GOOS == "windows" {
		logger.Info("windows dev environment detected",
			zap.String("log_dir", desktopDir),
		)
	} else {
		logger.Info("linux/mac dev environment detected",
			zap.String("log_dir", desktopDir),
		)
	}

	return ensureDir(desktopDir, logger)
}

func ensureDir(dir string, logger *zap.Logger) string {
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Warn("could not create log dir, using ./logs",
			zap.String("path", dir),
			zap.Error(err),
		)
		fallback := filepath.Join(".", "logs", serviceName)
		_ = os.MkdirAll(fallback, 0755)
		return fallback
	}
	logger.Info("log dir ready", zap.String("log_dir", dir))
	return dir
}

func isDocker() bool {
	_, err := os.Stat(dockerFlagFile)
	return err == nil
}

func (c *LogConsumer) Start(ctx context.Context) error {
	cfg := sarama.NewConfig()
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.NewBalanceStrategyRoundRobin(),
	}
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	group, err := sarama.NewConsumerGroup(c.brokers, c.groupID, cfg)
	if err != nil {
		return fmt.Errorf("creating consumer group: %w", err)
	}
	defer group.Close()

	c.logger.Info("kafka log consumer started",
		zap.Strings("brokers", c.brokers),
		zap.String("topic", c.topic),
		zap.String("log_dir", c.logDir),
	)

	handler := &consumerHandler{consumer: c}

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("kafka consumer shutting down")
			c.closeAllFiles()
			return nil
		default:
			if err := group.Consume(ctx, []string{c.topic}, handler); err != nil {
				c.logger.Error("consumer group error", zap.Error(err))
			}
		}
	}
}

func (c *LogConsumer) getFile(date string) (*os.File, error) {
	if f, ok := c.files[date]; ok {
		return f, nil
	}

	filename := fmt.Sprintf("log-%s.log", date)
	path := filepath.Join(c.logDir, filename)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening log file %s: %w", path, err)
	}

	c.logger.Info("opened log file", zap.String("file", path))
	c.files[date] = f
	return f, nil
}

func (c *LogConsumer) writeLog(date, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	f, err := c.getFile(date)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(f, message)
	return err
}

func (c *LogConsumer) closeAllFiles() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for date, f := range c.files {
		c.logger.Info("closing log file", zap.String("date", date))
		f.Close()
		delete(c.files, date)
	}
}

// ── Sarama ConsumerGroupHandler ───────────────────────────────────────────────

type consumerHandler struct {
	consumer *LogConsumer
}

func (h *consumerHandler) Setup(_ sarama.ConsumerGroupSession) error {
	return nil
}
func (h *consumerHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (h *consumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		date := string(msg.Key)
		if date == "" {
			date = time.Now().UTC().Format("2006-01-02")
		}

		if err := h.consumer.writeLog(date, string(msg.Value)); err != nil {
			h.consumer.logger.Error("writing log to file failed",
				zap.String("date", date),
				zap.Error(err),
			)
		}

		session.MarkMessage(msg, "")
	}
	return nil
}
