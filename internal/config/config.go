package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"golang.org/x/net/websocket"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
}

type ServerConfig struct {
	BaseURL string `env:"SERVER_URL" required:"true"`
	Port    string `env:"SERVER_PORT"`

	Auth AuthConfig
}

// ClientConfig will be set by the CLI app
type ClientConfig struct {
	Ports     []int  // The ports the client is tunneling
	ServerURL string // The server URL to connect to when handling tunnels
	Token     string // The auth token set VIA --login
}

type DatabaseConfig struct {
	Path string `env:"DB_PATH" required:"true"`
}

type AuthConfig struct {
	GithubClientId     string `env:"GITHUB_CLIENT_ID" required:"true"`
	GithubClientSecret string `env:"GITHUB_CLIENT_SECRET" required:"true"`
}

func LoadConfig() (*Config, error) {
	// We will manually validate the config values
	// We ignore the error as the .env file is optional
	_ = godotenv.Load()

	cfg := &Config{}

	// Server configuration
	baseURL := getOrDefault("SERVER_URL", "http://localhost")
	port := getOrDefault("SERVER_PORT", "8001")

	cfg.Server = ServerConfig{
		BaseURL: baseURL,
		Port:    port,
	}

	// Auth configuration
	githubClientId, err := getOrError("GITHUB_CLIENT_ID")
	if err != nil {
		return nil, err
	}
	githubClientSecret, err := getOrError("GITHUB_CLIENT_SECRET")
	if err != nil {
		return nil, err
	}

	cfg.Server.Auth = AuthConfig{
		GithubClientId:     githubClientId,
		GithubClientSecret: githubClientSecret,
	}

	// Database configuration
	dbPath := getOrDefault("DB_PATH", "tunol")
	cfg.Database = DatabaseConfig{
		Path: dbPath,
	}

	return cfg, nil
}

// Utility methods

// HTTPURL returns the full HTTP URL of the server
func (c *ServerConfig) HTTPURL() string {
	baseURL := strings.TrimSuffix(c.BaseURL, "/")
	if c.Port == "" || strings.Contains(c.BaseURL, "https://") { // On prod, we don't need to specify the port
		return baseURL
	}
	return fmt.Sprintf("%s:%s", baseURL, c.Port)
}

// WebSocketURL returns the WebSocket URL (ws:// or wss://) of the server for the client to connect to
func (c *ClientConfig) WebSocketURL() string {
	wsURL := strings.TrimSuffix(c.ServerURL, "/")
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	return wsURL + "/tunnel"
}

// NewWebSocketConfig creates a websocket.Config for CLI usage
func (c *ClientConfig) NewWebSocketConfig() (*websocket.Config, error) {
	// For CLI clients, we can use a simple static origin
	return websocket.NewConfig(c.WebSocketURL(), "https://cli.tunol.dev")
}

// getOrDefault returns the value of the environment variable with the given key
// or the default value if the variable is not set
func getOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getOrError returns the value of the environment variable with the given key
// or an error if the variable is not set
func getOrError(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("missing required environment variable %s", key)
	}
	return v, nil
}
