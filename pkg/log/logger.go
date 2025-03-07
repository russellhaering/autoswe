package log

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// Log is the global logger instance
	Log *zap.Logger
)

// Init initializes the global logger with the given config
func Init(development bool) error {
	var cfg zap.Config

	if development {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		cfg = zap.NewProductionConfig()
	}

	var err error
	Log, err = cfg.Build()
	if err != nil {
		return err
	}

	return nil
}

// With creates a child logger and adds structured context to it
func With(fields ...zap.Field) *zap.Logger {
	return Log.With(fields...)
}

// Debug uses fmt.Sprint to construct and log a message
func Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, fields...)
}

// Info uses fmt.Sprint to construct and log a message
func Info(msg string, fields ...zap.Field) {
	Log.Info(msg, fields...)
}

// Warn uses fmt.Sprint to construct and log a message
func Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, fields...)
}

// Error uses fmt.Sprint to construct and log a message
func Error(msg string, fields ...zap.Field) {
	Log.Error(msg, fields...)
}

// Fatal uses fmt.Sprint to construct and log a message, then calls os.Exit(1)
func Fatal(msg string, fields ...zap.Field) {
	Log.Fatal(msg, fields...)
}
