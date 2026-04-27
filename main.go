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
	logger, err := consoleCfg.Build(zap.AddCaller())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// ── Load config ───────────────────────────────────────────────────
	appCfg, err := config.Load(*cfgFile)
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
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
	paymentHandler, cleanup, err := wire.InitializeContainer(*cfgFile, logger)
	if err != nil {
		logger.Fatal("failed to initialize container", zap.Error(err))
	}
	defer cleanup()

	// ── gRPC server ───────────────────────────────────────────────────
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.UnaryRecovery(logger),
			middleware.UnaryLogger(logger),
			middleware.UnaryAuth(authClient, appCfg.AuthGRPC, logger), // validates JWT via auth-service gRPC
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

	httpSrv := &http.Server{
		Addr:    ":8081",
		Handler: middleware.HTTPLogger(logger)(mux),
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
