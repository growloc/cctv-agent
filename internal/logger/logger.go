package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cctv-agent/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger interface defines logging methods
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
	Fatal(msg string, keysAndValues ...interface{})
	With(keysAndValues ...interface{}) Logger
	Sync() error
}

// zapLogger wraps zap.SugaredLogger
type zapLogger struct {
	sugar *zap.SugaredLogger
}

// NewLogger creates a new logger instance with default settings
func NewLogger(level string) Logger {
	// Create default config
	cfg := &config.LoggerConfig{
		Level:         level,
		ConsoleOutput: true,
		ConsoleFormat: "text",
		FileOutput:    true,
		FileFormat:    "json",
		LogDir:        "logs",
		MaxSize:       100,
		MaxBackups:    3,
		MaxAge:        7,
		Compress:      true,
	}
	return NewLoggerWithConfig(cfg)
}

// NewLoggerWithConfig creates a new logger instance with custom configuration
func NewLoggerWithConfig(cfg *config.LoggerConfig) Logger {
	// Parse log level
	var zapLevel zapcore.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}
	
	// Create encoder configs
	jsonEncoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	
	textEncoderConfig := zapcore.EncoderConfig{
		TimeKey:        "T",
		LevelKey:       "L",
		NameKey:        "N",
		CallerKey:      "C",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "M",
		StacktraceKey:  "S",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05"),
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	
	var cores []zapcore.Core
	
	// Add console output if enabled
	if cfg.ConsoleOutput {
		var consoleEncoder zapcore.Encoder
		if cfg.ConsoleFormat == "json" {
			consoleEncoder = zapcore.NewJSONEncoder(jsonEncoderConfig)
		} else {
			consoleEncoder = zapcore.NewConsoleEncoder(textEncoderConfig)
		}
		consoleCore := zapcore.NewCore(
			consoleEncoder,
			zapcore.AddSync(os.Stdout),
			zapLevel,
		)
		cores = append(cores, consoleCore)
	}
	
	// Add file output if enabled
	if cfg.FileOutput {
		// Create log directory if it doesn't exist
		if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log directory: %v\n", err)
		}
		
		// Create lumberjack logger for log rotation
		lumberjackLogger := &lumberjack.Logger{
			Filename:   filepath.Join(cfg.LogDir, "cctv-agent.log"),
			MaxSize:    cfg.MaxSize,    // megabytes
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,     // days
			Compress:   cfg.Compress,
		}
		
		var fileEncoder zapcore.Encoder
		if cfg.FileFormat == "json" {
			fileEncoder = zapcore.NewJSONEncoder(jsonEncoderConfig)
		} else {
			fileEncoder = zapcore.NewConsoleEncoder(textEncoderConfig)
		}
		
		fileCore := zapcore.NewCore(
			fileEncoder,
			zapcore.AddSync(lumberjackLogger),
			zapLevel,
		)
		cores = append(cores, fileCore)
	}
	
	// Create tee core to write to multiple outputs
	core := zapcore.NewTee(cores...)
	
	// Create logger with AddCallerSkip to skip the wrapper functions
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	
	return &zapLogger{
		sugar: logger.Sugar(),
	}
}

// NewDevelopmentLogger creates a development logger
func NewDevelopmentLogger() Logger {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	
	return &zapLogger{
		sugar: logger.Sugar(),
	}
}

// NewFileLogger creates a logger that writes to a file
func NewFileLogger(level, filepath string) Logger {
	config := zap.NewProductionConfig()
	
	// Set log level
	logLevel := parseLogLevel(level)
	config.Level = zap.NewAtomicLevelAt(logLevel)
	
	// Configure encoder
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	
	// Output to file and stdout
	config.OutputPaths = []string{filepath, "stdout"}
	config.ErrorOutputPaths = []string{filepath, "stderr"}
	
	// Build logger
	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	
	return &zapLogger{
		sugar: logger.Sugar(),
	}
}

// Debug logs a debug message
func (l *zapLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.sugar.Debugw(msg, keysAndValues...)
}

// Info logs an info message
func (l *zapLogger) Info(msg string, keysAndValues ...interface{}) {
	l.sugar.Infow(msg, keysAndValues...)
}

// Warn logs a warning message
func (l *zapLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.sugar.Warnw(msg, keysAndValues...)
}

// Error logs an error message
func (l *zapLogger) Error(msg string, keysAndValues ...interface{}) {
	l.sugar.Errorw(msg, keysAndValues...)
}

// Fatal logs a fatal message and exits
func (l *zapLogger) Fatal(msg string, keysAndValues ...interface{}) {
	l.sugar.Fatalw(msg, keysAndValues...)
	os.Exit(1)
}

// With creates a child logger with additional fields
func (l *zapLogger) With(keysAndValues ...interface{}) Logger {
	return &zapLogger{
		sugar: l.sugar.With(keysAndValues...),
	}
}

// Sync flushes any buffered log entries
func (l *zapLogger) Sync() error {
	return l.sugar.Sync()
}

// parseLogLevel parses string log level to zapcore.Level
func parseLogLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// NopLogger is a no-op logger for testing
type NopLogger struct{}

// NewNopLogger creates a no-op logger
func NewNopLogger() Logger {
	return &NopLogger{}
}

func (n *NopLogger) Debug(msg string, keysAndValues ...interface{})  {}
func (n *NopLogger) Info(msg string, keysAndValues ...interface{})   {}
func (n *NopLogger) Warn(msg string, keysAndValues ...interface{})   {}
func (n *NopLogger) Error(msg string, keysAndValues ...interface{})  {}
func (n *NopLogger) Fatal(msg string, keysAndValues ...interface{})  {}
func (n *NopLogger) With(keysAndValues ...interface{}) Logger        { return n }
func (n *NopLogger) Sync() error                                     { return nil }
