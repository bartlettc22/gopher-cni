package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/bartlettc22/gopher-cni/internal/utils"
)

// Config holds global configuration for the application
type Config struct {
	Mode      string
	LogLevel  string
	LogFormat string
}

// LoadConfig parses command-line flags and environment variables to build configuration
func LoadConfig() *Config {
	if len(os.Args) < 2 {
		logging.Fatal(fmt.Errorf("subcommand is required, can be one of: daemon, init-validation, write-coredns-config"))
	} else if len(os.Args) > 2 {
		logging.Fatal(fmt.Errorf("exactly 1 argument (subcommand) is required, have %d", len(os.Args)-1))
	}
	config := &Config{
		Mode:      os.Args[1],
		LogLevel:  utils.GetEnv("LOG_LEVEL", "info"),
		LogFormat: utils.GetEnv("LOG_FORMAT", "text"),
	}

	return config
}

func configureDefaultLogger(config *Config) error {
	// Determine log level
	var logLevel slog.Level
	switch config.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		return fmt.Errorf("unknown log level: %s (valid levels: debug, info, warn, error)", config.LogLevel)
	}

	// Determine log handler
	var logHandler slog.Handler
	switch config.LogFormat {
	case "json":
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	case "text":
		logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	default:
		return fmt.Errorf("unknown log format: %s (valid formats: text, json)", config.LogFormat)
	}

	// Set default logger
	slog.SetDefault(slog.New(logHandler))

	return nil
}
