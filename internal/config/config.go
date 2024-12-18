package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
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

	UseSubdomains bool `env:"USE_SUBDOMAINS" default:"false"`

	Auth AuthConfig

	logLevel string `env:"LOG_LEVEL" default:"info"`
	Logger   *slog.Logger
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

	allowedLogLevels := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}

	cfg := &Config{}

	// Server configuration
	baseURL := getOrDefault("SERVER_URL", "http://localhost")
	port := getOrDefault("SERVER_PORT", "8001")
	logLevel := getOrDefault("LOG_LEVEL", "info")
	if _, ok := allowedLogLevels[logLevel]; !ok {
		return nil, fmt.Errorf("invalid log level: %s", logLevel)
	}
	useSubdomains := getOrDefault("USE_SUBDOMAINS", "false") == "true"

	cfg.Server = ServerConfig{
		BaseURL:       baseURL,
		Port:          port,
		UseSubdomains: useSubdomains,
		logLevel:      logLevel,
		Logger:        setupLogger(allowedLogLevels[logLevel]),
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

// setupLogger creates a new logger for the server application
func setupLogger(l slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     l,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				source := a.Value.Any().(*slog.Source)
				a.Value = slog.StringValue(source.File + ":" + strconv.Itoa(source.Line))
			}
			return a
		},
	}

	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}

// HTTPURL returns the full HTTP URL of the server
func (c *ServerConfig) HTTPURL() string {
	baseURL := strings.TrimSuffix(c.BaseURL, "/")
	if c.Port == "" || strings.Contains(c.BaseURL, "https://") { // On prod, we don't need to specify the port
		return baseURL
	}
	return fmt.Sprintf("%s:%s", baseURL, c.Port)
}

// SubdomainURL converts the server's base URL to a subdomainURL based on the id
// using the custom logic for local development and production
func (c *ServerConfig) SubdomainURL(id string) string {
	if !c.UseSubdomains {
		// Using path-based routing for local development
		baseURL := c.BaseURL
		if strings.HasPrefix(baseURL, "http://") {
			// For HTTP, include port
			if c.Port == "" {
				return fmt.Sprintf("%s/local/%s", baseURL, id)
			}
			return fmt.Sprintf("%s:%s/local/%s", baseURL, c.Port, id)
		}
		// For HTTPS, no port needed
		return fmt.Sprintf("%s/local/%s", baseURL, id)
	}

	// Using subdomain-based routing for production
	baseURL := strings.TrimPrefix(strings.TrimPrefix(c.BaseURL, "https://"), "http://")
	return fmt.Sprintf("https://%s.%s", id, baseURL)
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
