package auth

import (
	"fmt"
	"os"
	"path/filepath"
)

type TokenStore struct {
	configPath string
}

func NewTokenStore() (*TokenStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".tunol")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &TokenStore{
		configPath: filepath.Join(configDir, "token"),
	}, nil
}

func (s *TokenStore) StoreToken(token string) error {
	return os.WriteFile(s.configPath, []byte(token), 0600)
}

func (s *TokenStore) GetToken() (string, error) {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read token file: %w", err)
	}
	return string(data), nil
}
