package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// SetupLogger sets up the internal logger for the CLI tool
// It will use TUNOL_CONFIG_DIR if set, otherwise defaults to ~/.tunol/
func SetupLogger() *slog.Logger {
	var logsDir string

	// Get and check the actual HOME value
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	if configPath := os.Getenv("TUNOL_CONFIG_DIR"); configPath != "" {
		// If the path starts with $HOME, manually replace it
		fmt.Println("Config path before: ", configPath)
		if strings.HasPrefix(configPath, "$HOME") {
			configPath = strings.Replace(configPath, "$HOME", homeDir, 1)
		}
		fmt.Println("Config path after: ", configPath)
		logsDir = filepath.Join(configPath, "logs")
	} else {
		logsDir = filepath.Join(homeDir, ".tunol", "logs")
	}

	fmt.Printf("Final logs directory path: %s\n", logsDir)

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
