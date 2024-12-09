package tunnel

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jwtly10/go-tunol/pkg/config"
	"golang.org/x/net/websocket"
)

// Setup basic testing configuration
// For a unit test this should NOT require environment vars
func setupUnitTestEnv(t *testing.T) config.ServerConfig {
	t.Helper()

	return config.ServerConfig{
		Host:   "localhost",
		Port:   "8001",
		Scheme: "http",
	}
}

// TestServerStartAndAcceptConnections tests that the server starts and accepts connections
func TestServerStartAndAcceptConnections(t *testing.T) {
	cfg := setupUnitTestEnv(t)
	server := NewServer(":0", nil, &cfg) // Use a random port for testing (with stdout logger)

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsUrl := strings.Replace(ts.URL, "http", "ws", 1)

	ws, err := websocket.Dial(wsUrl, "", ts.URL)
	if err != nil {
		t.Fatalf("could not connect to websocket server: %v", err)
	}
	defer ws.Close()

	ping := Message{Type: MessageTypePing}
	if err := websocket.JSON.Send(ws, ping); err != nil {
		t.Fatalf("could not write message to websocket server: %v", err)
	}

	var reply Message
	if err := websocket.JSON.Receive(ws, &reply); err != nil {
		t.Fatalf("could not read message from websocket server: %v", err)
	}

	if reply.Type != MessageTypePong {
		t.Fatalf("expected pong message, got %s", reply.Type)
	}
}

// TestTunnelRegistration tests that the server correctly registers a new tunnel
func TestTunnelRegistration(t *testing.T) {
	cfg := setupUnitTestEnv(t)
	server := NewServer(":0", nil, &cfg) // Use a random port for testing (with stdout logger)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	wsUrl := strings.Replace(ts.URL, "http", "ws", 1)
	ws, err := websocket.Dial(wsUrl, "", ts.URL)
	if err != nil {
		t.Fatalf("could not connect to websocket server: %v", err)
	}
	defer ws.Close()

	req := Message{
		Type: MessageTypeTunnelReq,
		Payload: TunnelRequest{
			LocalPort: 8000,
		},
	}
	if err := websocket.JSON.Send(ws, req); err != nil {
		t.Fatalf("could not send tunnel request: %v", err)
	}

	var resp Message
	if err := websocket.JSON.Receive(ws, &resp); err != nil {
		t.Fatalf("could not receive tunnel response: %v", err)
	}

	if resp.Type != MessageTypeTunnelResp {
		t.Fatalf("expected tunnel response, got %s", resp.Type)
	}

	if resp.Payload == nil {
		t.Fatalf("expected payload in response, got nil")
	}

	var tunnelResp TunnelResponse
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

	if len(server.tunnels) != 1 {
		t.Fatalf("expected to have added 1 tunnel, got %d", len(server.tunnels))
	}
}
