package main

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newLogger creates a logger based on global flags loggerMode (dev|plain) and debug.
// dev: console encoder with development config (existing behavior)
// plain: minimal log format "LEVEL\tmessage".
func newLogger() *zap.Logger {
	switch loggerMode {
	case "plain":
		// Minimal encoder: only level and message separated by tab.
		encCfg := zapcore.EncoderConfig{}
		enc := zapcore.NewConsoleEncoder(encCfg)
		enabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool { return debug || lvl != zapcore.DebugLevel })
		core := zapcore.NewCore(enc, zapcore.Lock(os.Stderr), enabler)
		logger := zap.New(core)
		return logger
	case "dev":
		fallthrough
	default:
		consoleDebugging := zapcore.Lock(os.Stderr)
		consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
		consoleEnabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool { return debug || lvl != zapcore.DebugLevel })
		core := zapcore.NewCore(consoleEncoder, consoleDebugging, consoleEnabler)
		logger := zap.New(core)
		return logger
	}
}
