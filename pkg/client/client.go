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
	"time"

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
	// Close closes the specific tunnel instance
	Close() error
}

type EventHandler func(event Event)

type client struct {
	tunnels map[string]Tunnel
	events  EventHandler

	mu     sync.Mutex
	cfg    *config.ClientConfig
	logger *slog.Logger
}

func NewClient(cfg *config.ClientConfig, logger *slog.Logger, events EventHandler) Client {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return &client{
		tunnels: make(map[string]Tunnel),
		events:  events,

		cfg:    cfg,
		logger: logger,
	}
}

func (c *client) NewTunnel(localPort int) (Tunnel, error) {
	c.logger.Info("creating new tunnel", "localPort", localPort)

	headers := make(http.Header)
	if c.cfg.Token != "" {
		headers.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	// Create a manual ws config so we can add auth to handshake
	wsConfig, err := c.cfg.NewWebSocketConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create websocket config: %w", err)
	}

	if c.cfg.Token != "" {
		wsConfig.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	ws, err := websocket.DialConfig(wsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to tunol server: %w", err)
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
	// This should either be a success with tunnel details, or an error
	// In case of an error we end here
	switch resp.Type {
	case tunnel.MessageTypeTunnelResp:
		break
	case tunnel.MessageTypeError:
		var eEvent ErrorEvent
		b, err := json.Marshal(resp.Payload)
		if err != nil {
			return nil, fmt.Errorf("could not marshal error payload: %w", err)
		}
		if err := json.Unmarshal(b, &eEvent); err != nil {
			return nil, fmt.Errorf("could not unmarshal error payload: %w", err)
		}

		return nil, fmt.Errorf("failed to create tunnel: %s", eEvent.Error)
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
	// Clean up tunnel on exit
	defer func() {
		c.mu.Lock()
		// Close ws
		t.Close()
		delete(c.tunnels, t.url)
		c.mu.Unlock()
	}()

	for {
		var msg tunnel.Message
		if err := websocket.JSON.Receive(t.wsConn, &msg); err != nil {
			if c.events != nil {
				c.events(Event{
					Payload: RequestEvent{
						TunnelID:         t.url,
						Error:            "Client lost connection to server: " + err.Error(),
						Timestamp:        time.Now(),
						ConnectionFailed: true,
					},
				})
			}

			c.logger.Error("failed to receive websocket message", "error", err)
			return // This will trigger our deferred cleanup
		}

		switch msg.Type {
		case tunnel.MessageTypeError:
			c.logger.Error("received error message", "error", msg.Payload)
			var errMsg ErrorEvent
			b, err := json.Marshal(msg.Payload)
			if err != nil {
				c.logger.Error("failed to marshal error message", "error", err)
				continue
			}
			if err := json.Unmarshal(b, &errMsg); err != nil {
				c.logger.Error("failed to unmarshal error message", "error", err)
				continue
			}

			if c.events != nil {
				c.events(Event{
					Type:    EventTypeError,
					Payload: errMsg,
				})
			}

		case tunnel.MessageTypeHTTPRequest:
			c.logger.Debug("received HTTP request", "request", msg.Payload)
			startTime := time.Now()
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

				// Set the error message as 30 chars of the body, if status not OK
				var errMsg string
				if resp.StatusCode > 400 { // Some error status
					errMsg = string(body[:30])
					if len(body) > 30 {
						errMsg += "..."
					}
				}

				// Emit the event to be handled by the client impl
				if c.events != nil {
					c.events(Event{
						Type: EventTypeRequest,
						Payload: RequestEvent{
							TunnelID:  t.url,
							Method:    httpReq.Method,
							Path:      httpReq.Path,
							Status:    resp.StatusCode,
							Duration:  time.Since(startTime),
							Error:     errMsg,
							Timestamp: startTime,
						},
					})
				}
			}()

		case tunnel.MessageTypePing:
			c.logger.Debug("received ping message")
			if err := websocket.JSON.Send(t.wsConn, tunnel.Message{Type: tunnel.MessageTypePong}); err != nil {
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
	wsConn    *websocket.Conn
}

func (c *tunnelConn) URL() string {
	return c.url
}

func (c *tunnelConn) LocalPort() int {
	return c.localPort
}

func (c *tunnelConn) Close() error {
	if c.wsConn != nil {
		return c.wsConn.Close()
	}

	return nil
}
