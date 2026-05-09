package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/server"
	"go.uber.org/zap"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "train" {
		trainSmartRoute(os.Args[2:])
		return
	}

	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	logger, _ := zap.NewProduction()
	if os.Getenv("GATEWAY_LLM_DEBUG") == "true" {
		logger, _ = zap.NewDevelopment()
	}
	defer logger.Sync()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	if cfg.Logging.Level == "debug" {
		logger, _ = zap.NewDevelopment()
	}

	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Fatal("failed to create server", zap.Error(err))
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      srv.Router(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting Gateway-LLM",
			zap.Int("port", cfg.Server.Port),
			zap.String("version", "0.1.0"),
		)
		errCh <- httpServer.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	case err := <-errCh:
		logger.Error("server error", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.GracefulShutdownTimeout)
	defer cancel()

	logger.Info("shutting down gracefully", zap.Duration("timeout", cfg.Server.GracefulShutdownTimeout))
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", zap.Error(err))
	}

	srv.Close()
	logger.Info("server stopped")
}
