package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// TODO: Clean up the naming of all these hosts, ports etc, and consolidate it
// TODO: We use have multiple references to urls in different formats.

type Config struct {
	Server   ServerConfig
	Client   ClientConfig
	Database DatabaseConfig
}

type ServerConfig struct {
	URL    string
	Host   string
	Port   string
	Scheme string
	Auth   AuthConfig
}

type ClientConfig struct {
	Url    string
	Origin string
	Token  string // The token read from TODO: where should we store this?
}

type DatabaseConfig struct {
	Path string
}

type AuthConfig struct {
	GithubClientId     string `env:"GITHUB_CLIENT_ID" required:"true"`
	GithubClientSecret string `env:"GITHUB_CLIENT_SECRET" required:"true"`
	ServerHost         string `env:"SERVER_HOST" required:"true"`
}

func LoadConfig() (*Config, error) {
	// We will manually validate the config values
	// We ignore the error as the .env file is optional
	_ = godotenv.Load()

	cfg := &Config{}

	// Server configuration
	host := getOrDefault("SERVER_HOST", "localhost")
	scheme := getOrDefault("SERVER_SCHEME", "http")
	port := getOrDefault("SERVER_PORT", "8001")
	serverUrl := getOrDefault("SERVER_URL", fmt.Sprintf("%s://%s:%s", scheme, host, port))

	cfg.Server = ServerConfig{
		URL:    serverUrl,
		Host:   host,
		Port:   port,
		Scheme: scheme,
	}

	// Client configuration
	url, err := getOrError("CLIENT_URL")
	if err != nil {
		return nil, err
	}
	origin := getOrDefault("CLIENT_ORIGIN", "http://localhost")

	cfg.Client = ClientConfig{
		Url:    url,
		Origin: origin,
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
	dbPath := getOrDefault("DATABASE_PATH", "tunol")
	cfg.Database = DatabaseConfig{
		Path: dbPath,
	}

	return cfg, nil
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
