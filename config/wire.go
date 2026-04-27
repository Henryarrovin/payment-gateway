package config

import "github.com/google/wire"

var ProviderSet = wire.NewSet(
	Load,
	ProvideServerConfig,
	ProvideDatabaseConfig,
	ProvideAuthGRPCConfig,
	ProvideKafkaConfig,
)

func ProvideServerConfig(cfg *Config) ServerConfig {
	return cfg.Server
}
func ProvideDatabaseConfig(cfg *Config) DatabaseConfig {
	return cfg.Database
}
func ProvideAuthGRPCConfig(cfg *Config) AuthGRPCConfig {
	return cfg.AuthGRPC
}
func ProvideKafkaConfig(cfg *Config) KafkaConfig {
	return cfg.Kafka
}
