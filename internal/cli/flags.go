package cli

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/jwtly10/go-tunol/internal/config"
)

const (
	// Default prod URL
	defaultServerUrl = "https://tunol.dev"

	// Environment Variable for server URL
	// Locally we use http://localhost:8001
	serverUrlEnv = "TUNOL_SERVER_URL"
)

type portFlags []int

func (f *portFlags) String() string {
	return fmt.Sprint(*f)
}

func (f *portFlags) Set(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid port number: %v", err)
	}
	*f = append(*f, port)
	return nil
}

func ParseFlags() *config.ClientConfig {
	var (
		ports      portFlags
		loginToken string
		serverUrl  string
	)

	flag.Var(&ports, "port", "Port to tunnel (can be specified multiple times)")
	flag.StringVar(&loginToken, "login", "", "Login with the provided token")
	flag.StringVar(&serverUrl, "server", "", "Server URL")
	flag.Parse()

	return &config.ClientConfig{
		Ports:     []int(ports),
		Token:     loginToken,
		ServerURL: resolveServerUrl(serverUrl),
	}
}

func resolveServerUrl(serverUrl string) string {
	if serverUrl == "" {
		// If the server URL is not provided via the flag, check the environment
		serverUrl = os.Getenv(serverUrlEnv)
		if serverUrl == "" {
			// If the environment variable is stil not set, use the default
			serverUrl = defaultServerUrl
		}
	}

	return serverUrl
}
