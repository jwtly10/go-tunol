package client

import (
	"github.com/jwtly10/go-tunol/pkg/config"
	"github.com/jwtly10/go-tunol/pkg/tunnel"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
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

	client := NewClient(&cfg.Client, logger, func(event RequestEvent) {
		logger.Info("event", "event", event)
	})
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

	client := NewClient(&cfg.Client, logger, func(event RequestEvent) {
		logger.Info("event", "event", event)
	})
	defer client.Close()

	localURL, _ := url.Parse(localServer.URL)
	port, _ := strconv.Atoi(localURL.Port())

	tunnel, err := client.NewTunnel(port)
	if err != nil {
		t.Fatalf("failed to create tunnel: %v", err)
	}

	resp, err := http.Get(tunnel.URL() + "/")
	if err != nil {
		t.Fatalf("failed to make request through tunnel: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from local" {
		t.Errorf("got %s, want hello from local", string(body))
	}
}

func TestClientCallbacks(t *testing.T) {
	tests := []struct {
		name          string
		localHandler  http.HandlerFunc
		expectedEvent RequestEvent // We'll only check fields we can predict
		makeRequest   func(url string) (*http.Response, error)
	}{
		{
			name: "successful_request",
			localHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			},
			expectedEvent: RequestEvent{
				Method: "GET",
				Path:   "/",
				Status: http.StatusOK,
				Error:  "",
			},
			makeRequest: func(url string) (*http.Response, error) {
				return http.Get(url)
			},
		},
		{
			name: "local_server_error",
			localHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("internal error"))
			},
			expectedEvent: RequestEvent{
				Method: "GET",
				Path:   "/",
				Status: http.StatusInternalServerError,
				Error:  "internal error", // The error should be any body content
			},
			makeRequest: func(url string) (*http.Response, error) {
				return http.Get(url)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
			cfg := setupUnitTestEnv(t)
			server := tunnel.NewServer(logger, &cfg.Server)

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") == "websocket" {
					server.Handler().ServeHTTP(w, r)
				} else {
					server.ServeHTTP(w, r)
				}
			}))
			defer ts.Close()

			tsURL, _ := url.Parse(ts.URL)
			cfg.Server.Host = tsURL.Hostname()
			cfg.Server.Port = tsURL.Port()
			cfg.Server.Scheme = tsURL.Scheme
			cfg.Client.Url = strings.Replace(ts.URL, "http", "ws", 1)

			var localServer *httptest.Server
			if tc.localHandler != nil {
				localServer = httptest.NewServer(tc.localHandler)
				defer localServer.Close()
			} else {
				localServer = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			}

			eventChan := make(chan RequestEvent, 1)

			client := NewClient(&cfg.Client, logger, func(event RequestEvent) {
				eventChan <- event
			})
			defer client.Close()

			localURL, _ := url.Parse(localServer.URL)
			port, _ := strconv.Atoi(localURL.Port())

			tunnel, err := client.NewTunnel(port)
			if err != nil {
				t.Fatalf("failed to create tunnel: %v", err)
			}

			_, _ = tc.makeRequest(tunnel.URL() + "/") // For these tests, we will just call index

			var receivedEvent RequestEvent
			select {
			case receivedEvent = <-eventChan:
			case <-time.After(3 * time.Second):
				t.Fatal("timeout waiting for event")
			}

			// Check fields we can predict
			if receivedEvent.Method != tc.expectedEvent.Method {
				t.Errorf("got method %s, want %s", receivedEvent.Method, tc.expectedEvent.Method)
			}
			if receivedEvent.Path != tc.expectedEvent.Path {
				t.Errorf("got path %s, want %s", receivedEvent.Path, tc.expectedEvent.Path)
			}
			if receivedEvent.Status != tc.expectedEvent.Status {
				t.Errorf("got status %d, want %d", receivedEvent.Status, tc.expectedEvent.Status)
			}

			if tc.expectedEvent.Error != "" && !strings.Contains(receivedEvent.Error, tc.expectedEvent.Error) {
				t.Errorf("got error %q, want it to contain %q", receivedEvent.Error, tc.expectedEvent.Error)
			}

			if receivedEvent.TunnelID == "" {
				t.Error("TunnelID not set")
			}
			if receivedEvent.Duration == 0 {
				t.Error("Duration not set")
			}
			if receivedEvent.Timestamp.IsZero() {
				t.Error("Timestamp not set")
			}
		})
	}
}
