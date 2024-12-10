package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/jwtly10/go-tunol/pkg/config"
	"github.com/jwtly10/go-tunol/pkg/tunnel"
	"golang.org/x/net/websocket"
)

type Client interface {
	// NewTunnel creates a new tunnel and returns it
	NewTunnel(localPort int) (Tunnel, error)
	// Tunnels returns all active tunnels
	Tunnels() []Tunnel
	// Close cleans up and closes all active tunnels
	Close() error
}

type Tunnel interface {
	// URL returns the public URL of the tunnel
	URL() string
	// LocalPort returns the local port of the tunnel
	LocalPort() int
	// Status returns the current status of the tunnel
	Status() TunnelStatus
	// Close closes the specific tunnel instance
	Close() error
}

type TunnelStatus string

const (
	TunnelStatusConnected    TunnelStatus = "connected"
	TunnelStatusDisconnected TunnelStatus = "disconnected"
	TunnelStatusError        TunnelStatus = "error"
)

type client struct {
	tunnels map[string]Tunnel

	mu     sync.Mutex
	cfg    *config.ClientConfig
	logger *slog.Logger
}

func NewClient(cfg *config.ClientConfig, logger *slog.Logger) Client {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return &client{
		tunnels: make(map[string]Tunnel),

		cfg:    cfg,
		logger: logger,
	}
}

func (c *client) NewTunnel(localPort int) (Tunnel, error) {
	c.logger.Info("creating new tunnel", "localPort", localPort)
	ws, err := websocket.Dial(c.cfg.Url, "", c.cfg.Origin)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	req := tunnel.TunnelRequest{
		LocalPort: localPort,
	}

	if err := websocket.JSON.Send(ws, tunnel.Message{
		Type:    tunnel.MessageTypeTunnelReq,
		Payload: req,
	}); err != nil {
		return nil, fmt.Errorf("failed to send tunnel request: %w", err)
	}

	// Now wait for response of tunnel init
	var resp tunnel.Message
	if err := websocket.JSON.Receive(ws, &resp); err != nil {
		return nil, fmt.Errorf("failed to receive tunnel response: %w", err)
	}

	if resp.Type != tunnel.MessageTypeTunnelResp {
		return nil, fmt.Errorf("expected tunnel response, got %s", resp.Type)
	}

	b, err := json.Marshal(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("could not marshal payload: %w", err)
	}

	var tunnelResp tunnel.TunnelResponse
	if err := json.Unmarshal(b, &tunnelResp); err != nil {
		return nil, fmt.Errorf("could not unmarshal payload: %w", err)
	}

	t := &tunnelConn{
		url:       tunnelResp.URL,
		localPort: localPort,
		status:    TunnelStatusConnected,
		wsConn:    ws,
	}

	c.mu.Lock()
	c.tunnels[tunnelResp.URL] = t
	c.mu.Unlock()

	// Now we have created the tunnel we should start a goroutine to listen for messages
	go c.handleMessages(t)

	return t, nil
}

func (c *client) handleMessages(t *tunnelConn) {
	for {
		var msg tunnel.Message
		if err := websocket.JSON.Receive(t.wsConn, &msg); err != nil {
			t.status = TunnelStatusError
			c.logger.Error("failed to recieve websocket message", "error", err)
			return
		}

		switch msg.Type {
		case tunnel.MessageTypeHTTPRequest:
			// Parse the proxied request from messages
			var httpReq tunnel.HTTPRequest
			b, err := json.Marshal(msg.Payload)
			if err != nil {
				c.logger.Error("failed to marshal HTTP request", "error", err)
				continue
			}
			if err := json.Unmarshal(b, &httpReq); err != nil {
				c.logger.Error("failed to unmarshal HTTP request", "error", err)
				continue
			}

			// Forward the generated request to local host
			go func() {
				localURL := fmt.Sprintf("http://localhost:%d%s", t.localPort, httpReq.Path)
				// Build the request and headers
				req, err := http.NewRequest(httpReq.Method, localURL, bytes.NewReader(httpReq.Body))
				if err != nil {
					c.logger.Error("failed to create HTTP request", "error", err)
					return
				}

				for k, v := range httpReq.Headers {
					req.Header.Set(k, v)
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					c.logger.Error("failed to make HTTP request", "error", err)
					return
				}

				// Read the response body
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					c.logger.Error("failed to read response body", "error", err)
					return
				}

				// Build the response message
				headers := make(map[string]string)
				for k, v := range resp.Header {
					headers[k] = v[0]
				}

				wsResp := tunnel.Message{
					Type: tunnel.MessageTypeHTTPResponse,
					Payload: tunnel.HTTPResponse{
						StatusCode: resp.StatusCode,
						Headers:    headers,
						Body:       body,
						RequestId:  httpReq.RequestId,
					},
				}

				if err := websocket.JSON.Send(t.wsConn, wsResp); err != nil {
					c.logger.Error("failed to send HTTP response", "error", err)
					return
				}
			}()

		case tunnel.MessageTypePing:
			if err := websocket.JSON.Send(t.wsConn, tunnel.Message{Type: tunnel.MessageTypePong}); err != nil {
				t.status = TunnelStatusError
				c.logger.Error("failed to send websocket message", "error", err)
				return
			}
		}
	}
}

func (c *client) Tunnels() []Tunnel {
	c.mu.Lock()
	defer c.mu.Unlock()

	tunnels := make([]Tunnel, 0, len(c.tunnels))
	for _, t := range c.tunnels {
		tunnels = append(tunnels, t)
	}

	return tunnels
}

func (c *client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastErr error
	for _, t := range c.tunnels {
		if err := t.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

type tunnelConn struct {
	url       string
	localPort int
	status    TunnelStatus
	wsConn    *websocket.Conn
}

func (c *tunnelConn) URL() string {
	return c.url
}

func (c *tunnelConn) LocalPort() int {
	return c.localPort
}

func (c *tunnelConn) Status() TunnelStatus {
	return c.status
}

func (c *tunnelConn) Close() error {
	// TODO: Implement this method
	return nil
}
