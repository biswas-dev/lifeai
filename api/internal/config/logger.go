package config

import (
	"log"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// MustInitLogger builds the process logger: JSON in production, console
// elsewhere. It fatals rather than returning an error because there is no
// sensible way to run without a logger.
func MustInitLogger(env, level string) *zap.Logger {
	var cfg zap.Config
	if env == "production" {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err == nil {
		cfg.Level = zap.NewAtomicLevelAt(lvl)
	}
	logger, err := cfg.Build()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	return logger
}
