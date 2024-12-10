package tunnel

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
	server := NewServer(":0", logger, &cfg) // Use a random port for testing

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
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
	server := NewServer(":0", logger, &cfg) // Use a random port for testing
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

func TestHTTPForwarding(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := setupUnitTestEnv(t)
	server := NewServer(":8002", logger, &cfg)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			server.Handler().ServeHTTP(w, r)
		} else {
			server.ServeHTTP(w, r)
		}
	}))
	defer ts.Close()

	// Since we are actually testing the server, we need to overwrite the port to the test server port
	u, _ := url.Parse(ts.URL)
	cfg.Port = u.Port()

	// Connect as a WebSocket client (simulating the CLI)
	wsURL := strings.Replace(ts.URL, "http", "ws", 1)
	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Register a tunnel
	tunnelReq := Message{
		Type:    MessageTypeTunnelReq,
		Payload: TunnelRequest{LocalPort: 8000},
	}
	if err := websocket.JSON.Send(ws, tunnelReq); err != nil {
		t.Fatal(err)
	}

	// Get the tunnel URL
	var resp Message
	if err := websocket.JSON.Receive(ws, &resp); err != nil {
		t.Fatal(err)
	}
	var tunnelResp TunnelResponse
	b, _ := json.Marshal(resp.Payload)
	json.Unmarshal(b, &tunnelResp)

	// Capture any WebSocket messages (simulating the CLI)
	go func() {
		for {
			var msg Message
			if err := websocket.JSON.Receive(ws, &msg); err != nil {
				return
			}

			// When we get an HTTP request forwarded to us
			if msg.Type == MessageTypeHTTPRequest {
				// Get the requestId from the http request
				b, _ := json.Marshal(msg.Payload)
				var req HTTPRequest
				json.Unmarshal(b, &req)

				// Check the path is correct
				if req.Path != "/test/endpoint" {
					t.Errorf("the request path was parsed incorrectly, got %s, want /test/endpoint", req.Path)
				}

				// Mock local server response
				responseMsg := Message{
					Type: MessageTypeHTTPResponse,
					Payload: HTTPResponse{
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
