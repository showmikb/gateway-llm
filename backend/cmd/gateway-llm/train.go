package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/smartroute"
	"go.uber.org/zap"
)

// trainSmartRoute reads recent feedback events, joins them with the
// request audit log to reconstruct features, fits a logistic
// regression, and writes the resulting weights to disk for the next
// server startup to pick up.
//
// Usage: gateway-llm train [--config ...] [--out model.json] [--days N] [--metric <name>]
func trainSmartRoute(args []string) {
	fs := flag.NewFlagSet("train", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to configuration file")
	outPath := fs.String("out", "smartroute-model.json", "path to write the trained model")
	days := fs.Int("days", 30, "how many days of feedback to train on")
	metric := fs.String("metric", "quality", "feedback metric name to use as the training label")
	_ = fs.Parse(args)

	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database, err := db.New(ctx, cfg.Database.URL, cfg.Database.MaxConnections, logger)
	if err != nil {
		logger.Fatal("connect db", zap.Error(err))
	}
	defer database.Close()

	samples, err := database.LoadTrainingSamples(ctx, *metric, *days)
	if err != nil {
		logger.Fatal("load training samples", zap.Error(err))
	}
	logger.Info("loaded training data", zap.Int("samples", len(samples)))

	weights, err := smartroute.Train(samples, smartroute.TrainingOptions{
		Epochs: 300, LearningRate: 0.05, L2: 0.001,
	})
	if err != nil {
		logger.Fatal("train", zap.Error(err))
	}

	cls := smartroute.NewMLClassifier()
	if err := cls.SetWeights(weights, time.Now(), len(samples)); err != nil {
		logger.Fatal("set weights", zap.Error(err))
	}
	if err := cls.SaveFile(*outPath); err != nil {
		logger.Fatal("save", zap.Error(err))
	}

	fmt.Printf("trained smartroute model: samples=%d dim=%d written_to=%s\n",
		len(samples), smartroute.FeatureDim, *outPath)
}
