package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Server ServerConfig
	Client ClientConfig
}

type ServerConfig struct {
	Host   string
	Port   string
	Scheme string
}

type ClientConfig struct {
	Url    string
	Origin string
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

	cfg.Server = ServerConfig{
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
