package client

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jwtly10/go-tunol/pkg/config"
	"github.com/jwtly10/go-tunol/pkg/tunnel"
)

func setupUnitTestEnv(t *testing.T) config.Config {
	t.Helper()

	cfg := config.Config{}

	cfg.Server = config.ServerConfig{
		Host:   "localhost",
		Port:   "8001",
		Scheme: "http",
	}

	cfg.Client = config.ClientConfig{
		Url:    "ws://localhost:8001",
		Origin: "http://localhost",
	}

	return cfg
}

func TestCreateMultipleTunnels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
	server := tunnel.NewServer(logger, &cfg.Server)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http", "ws", 1)
	cfg.Client.Url = wsURL

	client := NewClient(&cfg.Client, logger)
	defer client.Close()

	tunnel1, err := client.NewTunnel(8080)
	if err != nil {
		t.Fatalf("failed to create first tunnel: %v", err)
	}

	tunnel2, err := client.NewTunnel(3000)
	if err != nil {
		t.Fatalf("failed to create second tunnel: %v", err)
	}

	tunnels := client.Tunnels()
	if len(tunnels) != 2 {
		t.Errorf("expected 2 tunnels, got %d", len(tunnels))
	}

	if tunnel1.URL() == tunnel2.URL() {
		t.Error("tunnel URLs should be unique")
	}

	if tunnel1.Status() != TunnelStatusConnected {
		t.Errorf("expected tunnel1 to be connected, got %s", tunnel1.Status())
	}
}

func TestHandleIncomingRequests(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
	server := tunnel.NewServer(logger, &cfg.Server)

	// Create test HTTP server with ws support
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			server.Handler().ServeHTTP(w, r)
		} else {
			server.ServeHTTP(w, r)
		}
	}))
	defer ts.Close()

	// Update server and client config to match generated test server
	tsURL, _ := url.Parse(ts.URL)
	cfg.Server.Host = tsURL.Hostname()
	cfg.Server.Port = tsURL.Port()
	cfg.Server.Scheme = tsURL.Scheme

	wsURL := strings.Replace(ts.URL, "http", "ws", 1)
	cfg.Client.Url = wsURL

	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from local"))
	}))
	defer localServer.Close()

	client := NewClient(&cfg.Client, logger)
	defer client.Close()

	localURL, _ := url.Parse(localServer.URL)
	port, _ := strconv.Atoi(localURL.Port())

	tunnel, err := client.NewTunnel(port)
	if err != nil {
		t.Fatalf("failed to create tunnel: %v", err)
	}

	resp, err := http.Get(tunnel.URL())
	if err != nil {
		t.Fatalf("failed to make request through tunnel: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from local" {
		t.Errorf("got %s, want hello from local", string(body))
	}
}
