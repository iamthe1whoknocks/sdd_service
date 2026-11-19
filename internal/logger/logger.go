package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New creates a configured Zap logger with a colored console encoder.
// Level sets the minimum log level (default: Info).
func New(level zapcore.Level) (*zap.Logger, error) {
	cfg := zap.NewProductionEncoderConfig()
	cfg.TimeKey = "time"
	cfg.EncodeTime = zapcore.RFC3339TimeEncoder
	cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder

	encoder := zapcore.NewConsoleEncoder(zapcore.EncoderConfig(cfg))

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		level,
	)

	return zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1)), nil
}
