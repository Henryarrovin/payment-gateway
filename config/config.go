package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	AuthGRPC AuthGRPCConfig
	Kafka    KafkaConfig
}

type ServerConfig struct {
	GRPCPort int    `mapstructure:"grpc_port"`
	Env      string `mapstructure:"env"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode,
	)
}

// AuthGRPCConfig holds coordinates for the auth-service gRPC endpoint.
type AuthGRPCConfig struct {
	Address         string        `mapstructure:"address"`          // e.g. auth-service:50051
	CanonicalSecret string        `mapstructure:"canonical_secret"` // shared HMAC secret
	ServiceName     string        `mapstructure:"service_name"`     // "payment-gateway"
	Timeout         time.Duration `mapstructure:"timeout"`
}

type KafkaConfig struct {
	Brokers []string `mapstructure:"brokers"`
	Topic   string   `mapstructure:"topic"`
	GroupID string   `mapstructure:"group_id"`
	LogDir  string   `mapstructure:"log_dir"`
	Enabled bool     `mapstructure:"enabled"`
}

func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// ── defaults ──────────────────────────────────
	v.SetDefault("server.grpc_port", 50052)
	v.SetDefault("server.env", "development")
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.sslmode", "disable")
	v.SetDefault("auth_grpc.address", "localhost:50051")
	v.SetDefault("auth_grpc.service_name", "payment-gateway")
	v.SetDefault("auth_grpc.timeout", "5s")
	v.SetDefault("kafka.enabled", false)
	v.SetDefault("kafka.topic", "payment-gateway-logs")
	v.SetDefault("kafka.group_id", "payment-log-consumer")
	v.SetDefault("kafka.log_dir", "./logs")
	v.SetDefault("kafka.brokers", []string{"localhost:9092"})

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
	}

	v.SetEnvPrefix("PAYMENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	// ── Manually read ALL env vars (mirrors auth-service approach) ──
	if val := os.Getenv("PAYMENT_DB_HOST"); val != "" {
		cfg.Database.Host = val
	}
	if val := os.Getenv("PAYMENT_DB_PORT"); val != "" {
		p, _ := strconv.Atoi(val)
		cfg.Database.Port = p
	}
	if val := os.Getenv("PAYMENT_DB_USER"); val != "" {
		cfg.Database.User = val
	}
	if val := os.Getenv("PAYMENT_DB_PASSWORD"); val != "" {
		cfg.Database.Password = val
	}
	if val := os.Getenv("PAYMENT_DB_NAME"); val != "" {
		cfg.Database.DBName = val
	}
	if val := os.Getenv("PAYMENT_DB_SSLMODE"); val != "" {
		cfg.Database.SSLMode = val
	}
	if val := os.Getenv("PAYMENT_AUTH_GRPC_ADDRESS"); val != "" {
		cfg.AuthGRPC.Address = val
	}
	if val := os.Getenv("PAYMENT_AUTH_GRPC_CANONICAL_SECRET"); val != "" {
		cfg.AuthGRPC.CanonicalSecret = val
	}
	if val := os.Getenv("PAYMENT_AUTH_GRPC_SERVICE_NAME"); val != "" {
		cfg.AuthGRPC.ServiceName = val
	}
	if val := os.Getenv("PAYMENT_AUTH_GRPC_TIMEOUT"); val != "" {
		d, _ := time.ParseDuration(val)
		cfg.AuthGRPC.Timeout = d
	}
	if val := os.Getenv("PAYMENT_SERVER_GRPC_PORT"); val != "" {
		p, _ := strconv.Atoi(val)
		cfg.Server.GRPCPort = p
	}
	if val := os.Getenv("PAYMENT_SERVER_ENV"); val != "" {
		cfg.Server.Env = val
	}
	if val := os.Getenv("PAYMENT_KAFKA_ENABLED"); val != "" {
		cfg.Kafka.Enabled = val == "true"
	}
	if val := os.Getenv("PAYMENT_KAFKA_BROKERS"); val != "" {
		cfg.Kafka.Brokers = strings.Split(val, ",")
	}
	if val := os.Getenv("PAYMENT_KAFKA_TOPIC"); val != "" {
		cfg.Kafka.Topic = val
	}
	if val := os.Getenv("PAYMENT_KAFKA_GROUP_ID"); val != "" {
		cfg.Kafka.GroupID = val
	}
	if val := os.Getenv("PAYMENT_KAFKA_LOG_DIR"); val != "" {
		cfg.Kafka.LogDir = val
	}

	return &cfg, nil
}
