package main

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func initLogger() *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	cfg.Sampling = &zap.SamplingConfig{Initial: 100, Thereafter: 100}
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.StacktraceKey = ""
	logger, _ := cfg.Build()
	return logger
}
