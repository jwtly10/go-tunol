package server

import (
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/proto"
	testutil "github.com/jwtly10/go-tunol/internal/testutils"
	"github.com/jwtly10/go-tunol/internal/web/user"
	"github.com/stretchr/testify/require"

	"github.com/jwtly10/go-tunol/internal/config"
	"golang.org/x/net/websocket"
)

// Setup basic testing configuration
// For a unit test this should NOT require environment vars
func setupUnitTestEnv(t *testing.T) config.ServerConfig {
	t.Helper()

	return config.ServerConfig{
		BaseURL: "http://localhost",
		Port:    "8001",
	}
}

// TestServerStartAndAcceptConnections tests that the server starts and accepts connections
func TestServerStartAndAcceptConnections(t *testing.T) {
	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
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

	// TODO: This is probably a terrible way to mock this
	tmpl := template.Must(template.New("test").Parse("test"))
	tunnelHandler := NewTunnelHandler(tokenService, tmpl, logger, &cfg)
	ts := httptest.NewServer(tunnelHandler.HandleWS())
	defer ts.Close()

	wsUrl := strings.Replace(ts.URL, "http", "ws", 1)

	// Create a manual ws config so we can add auth to handshake
	wsConfig, err := websocket.NewConfig(wsUrl, ts.URL)
	if err != nil {
		t.Fatalf("failed to create websocket config: %v", err)
	}

	wsConfig.Header.Set("Authorization", "Bearer "+token.PlainToken)

	ws, err := websocket.DialConfig(wsConfig)
	if err != nil {
		t.Fatalf("could not connect to websocket server: %v", err)
	}
	defer ws.Close()

	ping := proto.Message{Type: proto.MessageTypePing}
	if err := websocket.JSON.Send(ws, ping); err != nil {
		t.Fatalf("could not write message to websocket server: %v", err)
	}

	var reply proto.Message
	if err := websocket.JSON.Receive(ws, &reply); err != nil {
		t.Fatalf("could not read message from websocket server: %v", err)
	}

	if reply.Type != proto.MessageTypePong {
		t.Fatalf("expected pong message, got %s", reply.Type)
	}
}

// TestTunnelRegistration tests that the server correctly registers a new tunnel
func TestTunnelRegistration(t *testing.T) {
	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
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

	tmpl := template.Must(template.New("test").Parse("test"))
	tunnelHandler := NewTunnelHandler(tokenService, tmpl, logger, &cfg)
	ts := httptest.NewServer(tunnelHandler.HandleWS())
	defer ts.Close()

	wsUrl := strings.Replace(ts.URL, "http", "ws", 1)
	// Create a manual ws config so we can add auth to handshake
	wsConfig, err := websocket.NewConfig(wsUrl, ts.URL)
	if err != nil {
		t.Fatalf("failed to create websocket config: %v", err)
	}

	wsConfig.Header.Set("Authorization", "Bearer "+token.PlainToken)

	ws, err := websocket.DialConfig(wsConfig)
	if err != nil {
		t.Fatalf("could not connect to websocket server: %v", err)
	}
	defer ws.Close()

	req := proto.Message{
		Type: proto.MessageTypeTunnelReq,
		Payload: proto.TunnelRequest{
			LocalPort: 8000,
		},
	}
	if err := websocket.JSON.Send(ws, req); err != nil {
		t.Fatalf("could not send tunnel request: %v", err)
	}

	var resp proto.Message
	if err := websocket.JSON.Receive(ws, &resp); err != nil {
		t.Fatalf("could not receive tunnel response: %v", err)
	}

	if resp.Type != proto.MessageTypeTunnelResp {
		t.Fatalf("expected tunnel response, got %s", resp.Type)
	}

	if resp.Payload == nil {
		t.Fatalf("expected payload in response, got nil")
	}

	var tunnelResp proto.TunnelResponse
	b, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("could not marshal payload: %v", err)
	}

	if err := json.Unmarshal(b, &tunnelResp); err != nil {
		t.Fatalf("could not unmarshal payload: %v", err)
	}

	if !strings.HasPrefix(tunnelResp.URL, "http://localhost:8001") {
		t.Fatalf("expected URL to start with http://localhost:8001, got %s", tunnelResp.URL)
	}

	if len(tunnelResp.URL) < 21 {
		t.Fatalf("expected URL to be longer and include an ID, got %s", tunnelResp.URL)
	}

	if len(tunnelHandler.tunnels) != 1 {
		t.Fatalf("expected to have added 1 tunnel, got %d", len(tunnelHandler.tunnels))
	}
}

func TestHTTPForwarding(t *testing.T) {
	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
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

	tmpl := template.Must(template.New("test").Parse("test"))
	tunnelHandler := NewTunnelHandler(tokenService, tmpl, logger, &cfg)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			tunnelHandler.HandleWS().ServeHTTP(w, r)
		} else {
			tunnelHandler.ServeHTTP(w, r)
		}
	}))
	defer ts.Close()

	// Since we are actually testing the server, we need to overwrite the port to the test server port
	u, _ := url.Parse(ts.URL)
	cfg.Port = u.Port()

	// Connect as a WebSocket client (simulating the CLI)
	wsURL := strings.Replace(ts.URL, "http", "ws", 1)
	// Create a manual ws config so we can add auth to handshake
	wsConfig, err := websocket.NewConfig(wsURL, ts.URL)
	if err != nil {
		t.Fatalf("failed to create websocket config: %v", err)
	}

	wsConfig.Header.Set("Authorization", "Bearer "+token.PlainToken)

	ws, err := websocket.DialConfig(wsConfig)
	if err != nil {
		t.Fatalf("could not connect to websocket server: %v", err)
	}
	defer ws.Close()

	// Register a tunnel
	tunnelReq := proto.Message{
		Type:    proto.MessageTypeTunnelReq,
		Payload: proto.TunnelRequest{LocalPort: 8000},
	}
	if err := websocket.JSON.Send(ws, tunnelReq); err != nil {
		t.Fatal(err)
	}

	// Get the tunnel URL
	var resp proto.Message
	if err := websocket.JSON.Receive(ws, &resp); err != nil {
		t.Fatal(err)
	}
	var tunnelResp proto.TunnelResponse
	b, _ := json.Marshal(resp.Payload)
	json.Unmarshal(b, &tunnelResp)

	// Capture any WebSocket messages (simulating the CLI)
	go func() {
		for {
			var msg proto.Message
			if err := websocket.JSON.Receive(ws, &msg); err != nil {
				return
			}

			// When we get an HTTP request forwarded to us
			if msg.Type == proto.MessageTypeHTTPRequest {
				// Get the requestId from the http request
				b, _ := json.Marshal(msg.Payload)
				var req proto.HTTPRequest
				json.Unmarshal(b, &req)

				// Check the path is correct
				if req.Path != "/test/endpoint" {
					t.Errorf("the request path was parsed incorrectly, got %s, want /test/endpoint", req.Path)
				}

				// Mock local server response
				responseMsg := proto.Message{
					Type: proto.MessageTypeHTTPResponse,
					Payload: proto.HTTPResponse{
						StatusCode: 200,
						Headers:    map[string]string{"Content-Type": "text/plain"},
						Body:       []byte("Hello from local server"),
						RequestId:  req.RequestId,
					},
				}
				websocket.JSON.Send(ws, responseMsg)
			}
		}
	}()

	// Check we can access the local server through the tunnel
	res, err := http.Get(tunnelResp.URL + "/test/endpoint")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	if string(body) != "Hello from local server" {
		t.Errorf("got %s, want Hello from local server", string(body))
	}
}

// TestClientDisconnection verifies that the server properly cleans up when a client disconnects
func TestClientDisconnection(t *testing.T) {
	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
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

	tmpl := template.Must(template.New("test").Parse("test"))
	tunnelHandler := NewTunnelHandler(tokenService, tmpl, logger, &cfg)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			tunnelHandler.HandleWS().ServeHTTP(w, r)
		} else {
			tunnelHandler.ServeHTTP(w, r)
		}
	}))
	defer ts.Close()

	wsURL := strings.Replace(ts.URL, "http", "ws", 1)
	// Create a manual ws config so we can add auth to handshake
	wsConfig, err := websocket.NewConfig(wsURL, ts.URL)
	if err != nil {
		t.Fatalf("failed to create websocket config: %v", err)
	}

	wsConfig.Header.Set("Authorization", "Bearer "+token.PlainToken)

	client, err := websocket.DialConfig(wsConfig)
	if err != nil {
		t.Fatalf("could not connect to websocket server: %v", err)
	}
	defer client.Close()

	tunnelReq := proto.Message{
		Type:    proto.MessageTypeTunnelReq,
		Payload: proto.TunnelRequest{LocalPort: 8000},
	}
	if err := websocket.JSON.Send(client, tunnelReq); err != nil {
		t.Fatal(err)
	}

	// Get tunnel response
	var resp proto.Message
	if err := websocket.JSON.Receive(client, &resp); err != nil {
		t.Fatal(err)
	}

	// Verify tunnel was created
	if len(tunnelHandler.tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnelHandler.tunnels))
	}

	// Simulate the tunnelHandler closing
	client.Close()

	time.Sleep(100 * time.Millisecond)

	tunnelHandler.mu.Lock()
	finalTunnels := len(tunnelHandler.tunnels)
	tunnelHandler.mu.Unlock()

	if finalTunnels != 0 {
		t.Errorf("expected 0 tunnels after disconnect, got %d", finalTunnels)
	}
}
