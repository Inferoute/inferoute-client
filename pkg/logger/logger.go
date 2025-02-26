package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap logger
type Logger struct {
	*zap.Logger
	level zapcore.Level
}

// Config represents logger configuration
type Config struct {
	Level      string `yaml:"level"`
	LogDir     string `yaml:"log_dir"`
	MaxSize    int    `yaml:"max_size"`    // megabytes
	MaxBackups int    `yaml:"max_backups"` // number of backups
	MaxAge     int    `yaml:"max_age"`     // days
}

var defaultLogger *Logger

// New creates a new logger
func New(cfg *Config) (*Logger, error) {
	// Parse log level
	var level zapcore.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	// Create log directory if it doesn't exist
	logDir := cfg.LogDir
	if logDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		logDir = filepath.Join(homeDir, ".local", "state", "inferoute", "log")
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create encoder config
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Create core
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	fileEncoder := zapcore.NewJSONEncoder(encoderConfig)

	// Create log files
	logFile, err := os.OpenFile(
		filepath.Join(logDir, "inferoute.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	errorLogFile, err := os.OpenFile(
		filepath.Join(logDir, "error.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open error log file: %w", err)
	}

	// Create cores
	consoleCore := zapcore.NewCore(
		consoleEncoder,
		zapcore.AddSync(os.Stdout),
		level,
	)

	fileCore := zapcore.NewCore(
		fileEncoder,
		zapcore.AddSync(logFile),
		level,
	)

	errorCore := zapcore.NewCore(
		fileEncoder,
		zapcore.AddSync(errorLogFile),
		zapcore.ErrorLevel,
	)

	// Create logger
	core := zapcore.NewTee(consoleCore, fileCore, errorCore)
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &Logger{
		Logger: logger,
		level:  level,
	}, nil
}

// GetDefaultLogger returns the default logger
func GetDefaultLogger() *Logger {
	if defaultLogger == nil {
		// Create default logger
		cfg := &Config{
			Level: "info",
		}
		var err error
		defaultLogger, err = New(cfg)
		if err != nil {
			// If we can't create a logger, create a no-op logger
			defaultLogger = &Logger{
				Logger: zap.NewNop(),
				level:  zapcore.InfoLevel,
			}
		}
	}
	return defaultLogger
}

// SetDefaultLogger sets the default logger
func SetDefaultLogger(logger *Logger) {
	defaultLogger = logger
}

// Debug logs a debug message
func Debug(msg string, fields ...zapcore.Field) {
	GetDefaultLogger().Debug(msg, fields...)
}

// Info logs an info message
func Info(msg string, fields ...zapcore.Field) {
	GetDefaultLogger().Info(msg, fields...)
}

// Warn logs a warning message
func Warn(msg string, fields ...zapcore.Field) {
	GetDefaultLogger().Warn(msg, fields...)
}

// Error logs an error message
func Error(msg string, fields ...zapcore.Field) {
	GetDefaultLogger().Error(msg, fields...)
}

// Fatal logs a fatal message and exits
func Fatal(msg string, fields ...zapcore.Field) {
	GetDefaultLogger().Fatal(msg, fields...)
}

// With creates a child logger with the given fields
func With(fields ...zapcore.Field) *Logger {
	return &Logger{
		Logger: GetDefaultLogger().With(fields...),
		level:  GetDefaultLogger().level,
	}
}

// IsDebugEnabled returns true if debug logging is enabled
func IsDebugEnabled() bool {
	return GetDefaultLogger().level <= zapcore.DebugLevel
}
