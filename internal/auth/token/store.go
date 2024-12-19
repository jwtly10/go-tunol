package token

import (
	"fmt"
	"os"
	"path/filepath"
)

type Store struct {
	configPath string
}

// NewTokenStore sets up the internal token store for the CLI
// It will use TUNOL_CONFIG_DIR if set, otherwise defaults to ~/.tunol/
func NewTokenStore() (*Store, error) {
	if configPath := os.Getenv("TUNOL_CONFIG_DIR"); configPath != "" {
		if err := os.MkdirAll(configPath, 0700); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}
		return &Store{
			configPath: filepath.Join(configPath, "token"),
		}, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".tunol")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &Store{
		configPath: filepath.Join(configDir, "token"),
	}, nil
}

func (s *Store) StoreToken(token string) error {
	return os.WriteFile(s.configPath, []byte(token), 0600)
}

func (s *Store) GetToken() (string, error) {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read token file: %w", err)
	}
	return string(data), nil
}
