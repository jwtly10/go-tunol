package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jwtly10/go-tunol/internal/config"
	"github.com/jwtly10/go-tunol/internal/proto"

	"golang.org/x/net/websocket"
)

type TunnelManager interface {
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

type manager struct {
	tunnels map[string]Tunnel
	events  EventHandler

	mu     sync.Mutex
	cfg    *config.ClientConfig
	logger *slog.Logger
}

type tunnel struct {
	url       string
	localPort int
	wsConn    *websocket.Conn
}

func NewTunnelManager(cfg *config.ClientConfig, logger *slog.Logger, events EventHandler) TunnelManager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return &manager{
		tunnels: make(map[string]Tunnel),
		events:  events,

		cfg:    cfg,
		logger: logger,
	}
}

func (c *manager) NewTunnel(localPort int) (Tunnel, error) {
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

	req := proto.TunnelRequest{
		LocalPort: localPort,
	}

	if err := websocket.JSON.Send(ws, proto.Message{
		Type:    proto.MessageTypeTunnelReq,
		Payload: req,
	}); err != nil {
		return nil, fmt.Errorf("failed to send tunnel request: %w", err)
	}

	// Now wait for response of tunnel init
	var resp proto.Message
	if err := websocket.JSON.Receive(ws, &resp); err != nil {
		return nil, fmt.Errorf("failed to receive tunnel response: %w", err)
	}
	// This should either be a success with tunnel details, or an error
	// In case of an error we end here
	switch resp.Type {
	case proto.MessageTypeTunnelResp:
		break
	case proto.MessageTypeError:
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

	var tunnelResp proto.TunnelResponse
	if err := json.Unmarshal(b, &tunnelResp); err != nil {
		return nil, fmt.Errorf("could not unmarshal payload: %w", err)
	}

	t := &tunnel{
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

func (c *manager) handleMessages(t *tunnel) {
	// Clean up tunnel on exit
	defer func() {
		c.mu.Lock()
		// Close ws
		t.Close()
		delete(c.tunnels, t.url)
		c.mu.Unlock()
	}()

	for {
		var msg proto.Message
		if err := websocket.JSON.Receive(t.wsConn, &msg); err != nil {
			if c.events != nil {
				c.events(Event{
					Payload: RequestEvent{
						TunnelID:         t.url,
						Error:            "TunnelManager lost connection to server: " + err.Error(),
						Timestamp:        time.Now(),
						ConnectionFailed: true,
					},
				})
			}

			c.logger.Error("failed to receive websocket message", "error", err)
			return // This will trigger our deferred cleanup
		}

		switch msg.Type {
		case proto.MessageTypeError:
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

		case proto.MessageTypeHTTPRequest:
			c.logger.Debug("received HTTP request", "request", msg.Payload)
			startTime := time.Now()
			// Parse the proxied request from messages
			var httpReq proto.HTTPRequest
			b, err := json.Marshal(msg.Payload)
			if err != nil {
				c.logger.Error("failed to marshal HTTP request", "error", err)
				continue
			}
			if err := json.Unmarshal(b, &httpReq); err != nil {
				c.logger.Error("failed to unmarshal HTTP request", "error", err)
				continue
			}
			c.logger.Info("3. client received from websocket", "headers", httpReq.Headers)

			// Forward the generated request to local host
			go func() {
				localURL := fmt.Sprintf("http://localhost:%d%s", t.localPort, httpReq.Path)
				// Build the request and headers
				req, err := http.NewRequest(httpReq.Method, localURL, bytes.NewReader(httpReq.Body))
				if err != nil {
					c.logger.Error("failed to create HTTP request", "error", err)
					return
				}

				c.logger.Info("headers set when originally forwarding request to local", "headers", httpReq.Headers)

				// Here we need to carefully clean headers to avoid issues with conflicting headers
				// between cloudflare and any third party services

				isWebSocketUpgrade := strings.EqualFold(httpReq.Headers["Upgrade"], "websocket") &&
					strings.EqualFold(httpReq.Headers["Connection"], "upgrade")

					// Base headers that are always kept
				var headersToKeep = map[string]bool{
					"host":              true,
					"user-agent":        true,
					"accept":            true,
					"accept-encoding":   true,
					"accept-language":   true,
					"content-type":      true,
					"cookie":            true,
					"x-forwarded-for":   true,
					"x-forwarded-proto": true,
					"x-real-ip":         true,
					"authorization":     true,
				}

				// Add WebSocket specific headers if needed
				if isWebSocketUpgrade {
					headersToKeep["connection"] = true
					headersToKeep["upgrade"] = true
					headersToKeep["sec-websocket-key"] = true
					headersToKeep["sec-websocket-version"] = true
					headersToKeep["sec-websocket-protocol"] = true
					headersToKeep["sec-websocket-extensions"] = true
				}

				cleaned := make(map[string]string)
				for k, v := range httpReq.Headers {
					headerLower := strings.ToLower(k)
					if headersToKeep[headerLower] {
						cleaned[k] = v
					}
				}

				c.logger.Info("headers after cleaning", "headers", cleaned)

				for k, v := range cleaned {
					req.Header.Set(k, v)
				}

				c.logger.Info("4. making local request", "headers", cleaned)

				client := &http.Client{
					CheckRedirect: func(req *http.Request, via []*http.Request) error {
						return http.ErrUseLastResponse // Don't follow redirects
					},
				}

				resp, err := client.Do(req)
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

				headers := make(map[string]string)
				for k, v := range resp.Header {
					headers[k] = v[0]
				}

				if resp.StatusCode >= 300 && resp.StatusCode < 400 {
					location := resp.Header["Location"]
					c.logger.Info("redirect detected",
						"status_code", resp.StatusCode,
						"location", location,
						"original_path", httpReq.Path,
						"request_id", httpReq.RequestId)
				}

				c.logger.Info("5. local request response", "headers", headers)

				wsResp := proto.Message{
					Type: proto.MessageTypeHTTPResponse,
					Payload: proto.HTTPResponse{
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

				// Emit the event to be handled by the manager impl
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

		case proto.MessageTypePing:
			c.logger.Debug("received ping message")
			if err := websocket.JSON.Send(t.wsConn, proto.Message{Type: proto.MessageTypePong}); err != nil {
				c.logger.Error("failed to send websocket message", "error", err)
				return
			}
		}
	}
}

func (c *manager) Tunnels() []Tunnel {
	c.mu.Lock()
	defer c.mu.Unlock()

	tunnels := make([]Tunnel, 0, len(c.tunnels))
	for _, t := range c.tunnels {
		tunnels = append(tunnels, t)
	}

	return tunnels
}

func (c *manager) Close() error {
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

func (c *tunnel) URL() string {
	return c.url
}

func (c *tunnel) LocalPort() int {
	return c.localPort
}

func (c *tunnel) Close() error {
	if c.wsConn != nil {
		return c.wsConn.Close()
	}

	return nil
}
