package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// setupLogger sets up the internal logger for the CLI tool, logging to a file in the user's home directory
func SetupLogger() *slog.Logger {
	// Logs will be saved at ~/.tunol/logs/tunol-cli.log
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	logsDir := filepath.Join(homeDir, ".tunol", "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		fmt.Printf("Error creating logs directory: %v\n", err)
		os.Exit(1)
	}

	logFile := filepath.Join(logsDir, "tunol-cli.log")
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("Error opening log file: %v\n", err)
		os.Exit(1)
	}

	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewTextHandler(f, opts)
	logger := slog.New(handler)

	// TODO: Don't hardcode the version, we should have a way to bump the version properly
	logger.Info("tunol CLI started", "version", "0.1.0")

	return logger
}
