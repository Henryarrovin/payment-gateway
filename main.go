package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"payment-gateway/config"
	kafka "payment-gateway/kafka_logger_pipeline"
	"payment-gateway/middleware"
	paymentpb "payment-gateway/proto/paymentpb"
	"payment-gateway/wire"

	authpb "github.com/Henryarrovin/auth-service/proto/authpb"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found (continuing with system env)")
	}

	cfgFile := flag.String("config", "", "path to config file (optional)")
	flag.Parse()

	// ── Base console logger ───────────────────────────────────────────
	consoleCfg := zap.NewDevelopmentConfig()
	consoleCfg.EncoderConfig.CallerKey = "caller"
	consoleCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	baseLogger, err := consoleCfg.Build(zap.AddCaller())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build logger: %v\n", err)
		os.Exit(1)
	}
	defer baseLogger.Sync()

	// ── Load config ───────────────────────────────────────────────────
	appCfg, err := config.Load(*cfgFile)
	if err != nil {
		baseLogger.Fatal("failed to load config", zap.Error(err))
	}

	// ── Build logger: console + kafka tee ────────────────────────────
	logger, consumerCancel := buildLogger(appCfg, baseLogger)
	defer consumerCancel()
	if logger != baseLogger {
		defer logger.Sync()
	}

	// ── Auth service gRPC client ──────────────────────────────────────
	authConn, err := grpc.NewClient(
		appCfg.AuthGRPC.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		logger.Fatal("failed to connect to auth-service", zap.Error(err))
	}
	defer authConn.Close()

	authClient := authpb.NewAuthServiceClient(authConn)
	logger.Info("connected to auth-service", zap.String("address", appCfg.AuthGRPC.Address))

	// ── Dependency injection ──────────────────────────────────────────
	container, cleanup, err := wire.InitializeContainer(*cfgFile, logger)
	if err != nil {
		logger.Fatal("failed to initialize container", zap.Error(err))
	}
	defer cleanup()

	paymentHandler := container.PaymentHandler
	webhookHandler := container.WebhookHandler

	// ── gRPC server ───────────────────────────────────────────────────
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.UnaryRecovery(logger),
			middleware.UnaryLogger(logger),
			middleware.UnaryAuth(authClient, appCfg.AuthGRPC, logger),
		),
	)
	paymentpb.RegisterPaymentServiceServer(grpcSrv, paymentHandler)
	reflection.Register(grpcSrv)

	grpcAddr := fmt.Sprintf(":%d", appCfg.Server.GRPCPort)
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Fatal("grpc listen failed", zap.Error(err))
	}

	// ── HTTP gateway ──────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := runtime.NewServeMux()

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := paymentpb.RegisterPaymentServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		logger.Fatal("gateway registration failed", zap.Error(err))
	}

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/v1/payments/webhook", webhookHandler)
	rootMux.Handle("/", mux)

	httpSrv := &http.Server{
		Addr:    ":8081",
		Handler: middleware.HTTPLogger(logger)(rootMux),
	}

	// ── Start gRPC ────────────────────────────────────────────────────
	grpcReady := make(chan struct{})
	go func() {
		logger.Info("gRPC listening", zap.String("addr", grpcAddr))
		close(grpcReady)
		if err := grpcSrv.Serve(grpcLis); err != nil {
			logger.Fatal("grpc serve failed", zap.Error(err))
		}
	}()
	<-grpcReady

	// ── Start HTTP ────────────────────────────────────────────────────
	go func() {
		logger.Info("HTTP gateway listening", zap.String("addr", ":8081"))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http serve failed", zap.Error(err))
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down payment-gateway…")
	grpcSrv.GracefulStop()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		logger.Error("http shutdown error", zap.Error(err))
	}
	logger.Info("payment-gateway stopped")
}

// buildLogger creates a tee logger: console + kafka (if enabled).
// Returns the logger and a cancel func to stop the kafka consumer.
func buildLogger(appCfg *config.Config, baseLogger *zap.Logger) (*zap.Logger, func()) {
	if !appCfg.Kafka.Enabled {
		baseLogger.Info("kafka logging disabled, using console only")
		return baseLogger, func() {}
	}

	kafkaCore, err := kafka.NewKafkaCore(
		appCfg.Kafka.Brokers,
		appCfg.Kafka.Topic,
		zapcore.InfoLevel,
	)
	if err != nil {
		baseLogger.Error("kafka connection failed, using console only", zap.Error(err))
		return baseLogger, func() {}
	}

	// Tee: console + kafka
	logger := zap.New(
		zapcore.NewTee(baseLogger.Core(), kafkaCore),
		zap.AddCaller(),
		zap.AddCallerSkip(0),
	)

	baseLogger.Info("kafka connected",
		zap.Strings("brokers", appCfg.Kafka.Brokers),
		zap.String("topic", appCfg.Kafka.Topic),
		zap.String("log_dir", appCfg.Kafka.LogDir),
	)

	// Start consumer — reads from Kafka and writes to disk
	consumer := kafka.NewLogConsumer(
		appCfg.Kafka.Brokers,
		appCfg.Kafka.Topic,
		appCfg.Kafka.GroupID,
		appCfg.Kafka.LogDir,
		baseLogger,
	)

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	go func() {
		if err := consumer.Start(consumerCtx); err != nil {
			baseLogger.Error("kafka consumer stopped", zap.Error(err))
		}
	}()

	return logger, consumerCancel
}
