package client

import (
	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/config"
	"github.com/jwtly10/go-tunol/internal/server"
	testutil "github.com/jwtly10/go-tunol/internal/testutils"
	"github.com/jwtly10/go-tunol/internal/utils"
	"github.com/jwtly10/go-tunol/internal/web/user"
	"github.com/stretchr/testify/require"
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

func setupUnitTestEnv(t *testing.T) (*config.ServerConfig, *config.ClientConfig) {
	t.Helper()

	s := config.ServerConfig{
		BaseURL: "http://localhost",
		Port:    "8001",
	}

	c := config.ClientConfig{
		ServerURL: "http://localhost:8001",
	}

	return &s, &c
}

// TestCanCreateTunnels tests that the manager can create tunnels, using the systems auth process
func TestCanCreateTunnels(t *testing.T) {
	// Setup all dependencies needed for the flow
	sCfg, cCfg := setupUnitTestEnv(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	tokenService := token.NewTokenService(db)
	userRepo := user.NewUserRepository(db)

	// Create test user
	user := &user.User{
		ID:              0,
		GithubID:        12345,
		GithubUsername:  "testuser",
		GithubAvatarURL: "https://github.com/avatar.jpg",
		GithubEmail:     "test@example.com",
	}

	user, err := userRepo.CreateUser(user)
	require.NoError(t, err)

	// Create auth token
	token, err := tokenService.CreateToken(user.ID, "Test token", 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	cCfg.Token = token.PlainToken

	// Set up test
	tunnelHandler := server.NewTunnelHandler(tokenService, logger, sCfg)
	ts := httptest.NewServer(tunnelHandler.HandleWS())
	defer ts.Close()
	// update the manager config to point to test server
	cCfg.ServerURL = ts.URL

	// Do test
	client := NewTunnelManager(cCfg, logger, func(event Event) {
		logger.Info("event", "event", event)
	})
	defer client.Close()

	// Create some tunnels and assert they are unique and valid
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
}

// TestHandleIncomingRequests tests that the manager can handle incoming requests
// and correctly forward them to the local server
func TestHandleIncomingRequests(t *testing.T) {
	// Setup all dependencies needed for the flow
	s, c := setupUnitTestEnv(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	tokenService := token.NewTokenService(db)
	userRepo := user.NewUserRepository(db)

	// Create test user
	user := &user.User{
		ID:              0,
		GithubID:        12345,
		GithubUsername:  "testuser",
		GithubAvatarURL: "https://github.com/avatar.jpg",
		GithubEmail:     "test@example.com",
	}

	user, err := userRepo.CreateUser(user)
	require.NoError(t, err)

	// Create auth token
	token, err := tokenService.CreateToken(user.ID, "Test token", 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	c.Token = token.PlainToken

	tunnelHandler := server.NewTunnelHandler(tokenService, logger, s)
	// Create test HTTP server with ws support
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			tunnelHandler.HandleWS().ServeHTTP(w, r)
		} else {
			tunnelHandler.ServeHTTP(w, r)
		}
	}))
	defer ts.Close()

	// Update server and manager config to match generated test server
	tsURL, _ := url.Parse(ts.URL)
	s.BaseURL = tsURL.Scheme + "://" + tsURL.Hostname()
	s.Port = tsURL.Port()
	c.ServerURL = ts.URL

	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from local"))
	}))
	defer localServer.Close()

	client := NewTunnelManager(c, logger, func(event Event) {
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

// TestClientAuthentication tests that the manager can authenticate with the server
// and correctly handle should the server reject the initial connection
func TestClientAuthentication(t *testing.T) {
	tests := []struct {
		name            string
		expectedError   bool
		errorIfExpected string
		token           string
	}{
		{
			name:          "successful_request",
			expectedError: false,
			token:         "real-token",
		},
		{
			name:            "missing_token",
			expectedError:   true,
			errorIfExpected: "no token provided",
			token:           "",
		},
		{
			name:            "invalid_token",
			expectedError:   true,
			errorIfExpected: "does not exist",
			token:           "invalid-token",
		},
	}

	// Setup all dependencies needed for the flow
	s, c := setupUnitTestEnv(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	tokenService := token.NewTokenService(db)
	userRepo := user.NewUserRepository(db)

	// Create test user
	user := &user.User{
		ID:              0,
		GithubID:        12345,
		GithubUsername:  "testuser",
		GithubAvatarURL: "https://github.com/avatar.jpg",
		GithubEmail:     "test@example.com",
	}

	user, err := userRepo.CreateUser(user)
	require.NoError(t, err)

	// Create auth token, do manually so we can test missing token
	hash := utils.HashToken("real-token")
	_, err = db.Exec(`
        INSERT INTO tokens (user_id, token_hash, description, created_at, expires_at)
        VALUES (?, ?, ?, ?, ?)
    `, 0, hash, "test token", time.Now(), time.Now().Add(24*time.Hour))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set the manager token based on the test
			c.Token = tc.token

			tunnelHandler := server.NewTunnelHandler(tokenService, logger, s)
			ts := httptest.NewServer(tunnelHandler.HandleWS())
			defer ts.Close()

			tsURL, _ := url.Parse(ts.URL)
			s.BaseURL = tsURL.Scheme + "://" + tsURL.Hostname()
			s.Port = tsURL.Port()
			c.ServerURL = ts.URL

			eventChan := make(chan Event, 1)
			client := NewTunnelManager(c, logger, func(event Event) {
				eventChan <- event
			})
			defer client.Close()

			_, err := client.NewTunnel(9000)
			if tc.expectedError {
				if err == nil {
					t.Fatalf("for test %s, expected error like %s, got nil", tc.name, tc.errorIfExpected)
				}

				if !strings.Contains(err.Error(), tc.errorIfExpected) {
					t.Fatalf("for test %s, expected error like %s, got %v", tc.name, tc.errorIfExpected, err)
				}
			}
			if !tc.expectedError && err != nil {
				t.Fatalf("for test %s, expected no error, got %v", tc.name, err)
			}
		})
	}
}

// TestClientEvents tests that the manager emits events for each request made through a tunnel
func TestClientRequestEvents(t *testing.T) {
	tests := []struct {
		name          string
		localHandler  http.HandlerFunc
		expectedEvent Event
		makeRequest   func(url string) (*http.Response, error)
	}{
		{
			name: "successful_request",
			localHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			},
			expectedEvent: Event{
				Type: EventTypeRequest,
				Payload: RequestEvent{
					Method: "GET",
					Path:   "/",
					Status: http.StatusOK,
					Error:  "",
				},
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
			expectedEvent: Event{
				Type: EventTypeRequest,
				Payload: RequestEvent{
					Method: "GET",
					Path:   "/",
					Status: http.StatusInternalServerError,
					Error:  "internal error", // The error should be any body content
				},
			},
			makeRequest: func(url string) (*http.Response, error) {
				return http.Get(url)
			},
		},
	}

	// Setup all dependencies needed for the flow
	s, c := setupUnitTestEnv(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	tokenService := token.NewTokenService(db)
	userRepo := user.NewUserRepository(db)

	// Create test user
	user := &user.User{
		ID:              0,
		GithubID:        12345,
		GithubUsername:  "testuser",
		GithubAvatarURL: "https://github.com/avatar.jpg",
		GithubEmail:     "test@example.com",
	}

	user, err := userRepo.CreateUser(user)
	require.NoError(t, err)

	// Create auth token
	token, err := tokenService.CreateToken(user.ID, "Test token", 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	c.Token = token.PlainToken

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tunnelHandler := server.NewTunnelHandler(tokenService, logger, s)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") == "websocket" {
					tunnelHandler.HandleWS().ServeHTTP(w, r)
				} else {
					tunnelHandler.ServeHTTP(w, r)
				}
			}))
			defer ts.Close()

			tsURL, _ := url.Parse(ts.URL)
			s.BaseURL = tsURL.Scheme + "://" + tsURL.Hostname()
			s.Port = tsURL.Port()
			c.ServerURL = ts.URL

			var localServer *httptest.Server
			if tc.localHandler != nil {
				localServer = httptest.NewServer(tc.localHandler)
				defer localServer.Close()
			} else {
				localServer = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			}

			eventChan := make(chan Event, 1)

			client := NewTunnelManager(c, logger, func(event Event) {
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

			var receivedEvent Event
			select {
			case receivedEvent = <-eventChan:
			case <-time.After(3 * time.Second):
				t.Fatal("timeout waiting for event")
			}

			// Check fields we can predict

			if receivedEvent.Type != tc.expectedEvent.Type {
				t.Errorf("got type %s, want %s", receivedEvent.Type, tc.expectedEvent.Type)
			}

			receivedEventPayload := receivedEvent.Payload.(RequestEvent)
			if receivedEventPayload.Method != tc.expectedEvent.Payload.(RequestEvent).Method {
				t.Errorf("got method %s, want %s", receivedEventPayload.Method, tc.expectedEvent.Payload.(RequestEvent).Method)
			}

			if receivedEventPayload.Path != tc.expectedEvent.Payload.(RequestEvent).Path {
				t.Errorf("got path %s, want %s", receivedEventPayload.Path, tc.expectedEvent.Payload.(RequestEvent).Path)
			}

			if receivedEventPayload.Status != tc.expectedEvent.Payload.(RequestEvent).Status {
				t.Errorf("got status %d, want %d", receivedEventPayload.Status, tc.expectedEvent.Payload.(RequestEvent).Status)
			}

			if tc.expectedEvent.Payload.(RequestEvent).Error != "" && !strings.Contains(receivedEventPayload.Error, tc.expectedEvent.Payload.(RequestEvent).Error) {
				t.Errorf("got error %q, want it to contain %q", receivedEventPayload.Error, tc.expectedEvent.Payload.(RequestEvent).Error)
			}
			if receivedEvent.Payload.(RequestEvent).TunnelID == "" {
				t.Error("TunnelID not set")
			}

			if receivedEvent.Payload.(RequestEvent).Duration == 0 {
				t.Error("Duration not set")
			}
			if receivedEvent.Payload.(RequestEvent).Timestamp.IsZero() {
				t.Error("Timestamp not set")
			}
		})
	}
}
