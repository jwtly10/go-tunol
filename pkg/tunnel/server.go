package tunnel

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jwtly10/go-tunol/pkg/config"
	"golang.org/x/net/websocket"
)

type Server struct {
	tunnels         map[string]*Tunnel
	pendingRequests map[string]chan *HTTPResponse

	mu     sync.Mutex
	logger *slog.Logger
	cfg    *config.ServerConfig
	done   chan struct{} // Signal for cleanup goroutine
}

type Tunnel struct {
	ID           string
	LocalPort    int
	WSConn       *websocket.Conn
	Path         string    // For local dev & pre-subdomain routing
	UrlPrefix    string    // For subdomain routing
	LastActivity time.Time // For tracking healthy connections
	Created      time.Time
}

func NewServer(logger *slog.Logger, cfg *config.ServerConfig) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	s := &Server{
		tunnels:         make(map[string]*Tunnel),
		pendingRequests: make(map[string]chan *HTTPResponse),

		logger: logger,
		cfg:    cfg,
	}

	go s.cleanupLoop()

	return s
}

func (s *Server) Handler() http.Handler {
	return websocket.Handler(s.handleWS)
}

func (s *Server) handleWS(ws *websocket.Conn) {
	defer func() {
		s.mu.Lock()
		// Clean up all tunnels associated with this connection
		for id, tunnel := range s.tunnels {
			if tunnel.WSConn == ws {
				s.logger.Info("cleaning up disconnected tunnel", "id", id, "total", len(s.tunnels)-1)
				tunnel.WSConn.Close()
				delete(s.tunnels, id)

				// Clean up any pending requests for this tunnel
				for reqID, ch := range s.pendingRequests {
					if strings.HasPrefix(reqID, id) {
						close(ch)
						delete(s.pendingRequests, reqID)
					}
				}
			}
		}
		s.mu.Unlock()
	}()

	for {
		var msg Message
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			var id string
			for _, tunnel := range s.tunnels {
				if tunnel.WSConn == ws {
					id = tunnel.ID
				}
			}
			if err == io.EOF {
				s.logger.Info("client disconnected", "id", id, "error", err)
			} else {
				s.logger.Info("websocket error", "id", id, "error", err)
			}
			return // Trigger deferred clean up
		}

		if msg.Type == MessageTypeHTTPResponse {
			var resp HTTPResponse
			if b, err := json.Marshal(msg.Payload); err == nil {
				if err := json.Unmarshal(b, &resp); err == nil {
					s.mu.Lock()
					for _, tunnel := range s.tunnels {
						if tunnel.WSConn == ws {
							tunnel.LastActivity = time.Now()
						}
					}
					s.mu.Unlock()
				}
			}
		}

		switch msg.Type {
		case MessageTypePing:
			s.logger.Info("received ping message")
			if err := websocket.JSON.Send(ws, Message{Type: MessageTypePong}); err != nil {
				s.logger.Error("failed to send websocket message", "error", err)
				return
			}

		case MessageTypePong:
			s.logger.Info("received pong message")

		case MessageTypeTunnelReq:
			s.logger.Info("received tunnel request", "payload", msg.Payload)
			var req TunnelRequest
			b, err := json.Marshal(msg.Payload)
			if err != nil {
				s.logger.Error("failed to marshal tunnel request", "error", err)
				return
			}

			if err := json.Unmarshal(b, &req); err != nil {
				s.logger.Error("failed to unmarshal tunnel request", "error", err)
			}

			id := generateID()

			t := &Tunnel{
				ID:           id,
				LocalPort:    req.LocalPort,
				WSConn:       ws,
				Path:         s.cfg.Scheme + "://" + s.cfg.Host + ":" + s.cfg.Port + "/" + id,
				LastActivity: time.Now(),
				Created:      time.Now(),
			}

			s.mu.Lock()
			s.tunnels[id] = t
			s.mu.Unlock()

			resp := Message{
				Type: MessageTypeTunnelResp,
				Payload: TunnelResponse{
					URL: t.Path,
				},
			}

			if err := websocket.JSON.Send(ws, resp); err != nil {
				s.logger.Error("failed to send tunnel response", "error", err)
			}

			s.logger.Info("new tunnel registered", "totalTunnels", len(s.tunnels), "id", id, "localPort", req.LocalPort, "url", t.Path)

		case MessageTypeHTTPResponse:
			s.logger.Info("received http response from tunnel", "payload", msg.Payload)
			var resp HTTPResponse
			b, _ := json.Marshal(msg.Payload)
			if err := json.Unmarshal(b, &resp); err != nil {
				s.logger.Error("failed to unmarshal HTTP response", "error", err)
				continue
			}

			s.mu.Lock()
			if ch, exists := s.pendingRequests[resp.RequestId]; exists {
				ch <- &resp
				delete(s.pendingRequests, resp.RequestId)
			}
			s.mu.Unlock()

		default:
			s.logger.Warn("unknown message type", "type", msg.Type, "content", msg)
		}
	}
}

type HTTPRequest struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      []byte            `json:"body"`
	RequestId string            `json:"request_id"`
}

type HTTPResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
	RequestId  string            `json:"request_id"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("received http request", "method", r.Method, "rawPath", r.URL.Path)

	// The url will look like this // TODO: In future it will be subdomain based
	// http://localhost:8001/tunnel_id/some_external_path/and/maybe/more
	tunnelId, realPath, err := extractTunnelIDAndPath(r.URL.String())
	if err != nil {
		s.logger.Error("failed to extract tunnel_id from url", "error", err)
	}

	s.mu.Lock()
	tunnel, exists := s.tunnels[tunnelId]
	s.mu.Unlock()

	if !exists {
		s.logger.Warn("tunnel not found", "id", tunnelId)
		http.NotFound(w, r)
		return
	}

	// We need to be able to wait for the response from the CLI tunnel
	respChan := make(chan *HTTPResponse, 1)
	requestId := generateID()

	s.mu.Lock()
	s.pendingRequests[requestId] = respChan
	s.mu.Unlock()

	// Clean up the pending request once done
	defer func() {
		s.mu.Lock()
		delete(s.pendingRequests, requestId)
		s.mu.Unlock()
	}()

	// Map the HTTP request to a WS message
	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = v[0]
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	httpReq := HTTPRequest{
		Method:    r.Method,
		Path:      realPath,
		Body:      body,
		Headers:   headers,
		RequestId: requestId,
	}

	s.logger.Info("forwarding http request to tunnel", "tunnel_id", tunnelId, "request_id", requestId, "path", realPath)

	msg := Message{
		Type:    MessageTypeHTTPRequest,
		Payload: httpReq,
	}

	if err := websocket.JSON.Send(tunnel.WSConn, msg); err != nil {
		http.Error(w, "Failed to forward request", http.StatusInternalServerError)
		return
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		// Response has been received
		// Write the response back to the client
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(resp.Body)
	case <-time.After(5 * time.Second):
		http.Error(w, "Request timed out", http.StatusGatewayTimeout)
	}
}

// Shutdown provides a way to gracefully shutdown the server
func (s *Server) Shutdown() {
	close(s.done)
	s.cleanupDeadConnections()
}

// cleanupLoop periodically checks for dead connections and cleans them up
func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupDeadConnections()
		case <-s.done:
			return
		}
	}
}

// isConnClosed pings the websocket connection to check if it's still alive
func (s *Server) isConnClosed(ws *websocket.Conn) bool {
	err := websocket.JSON.Send(ws, Message{Type: MessageTypePing})
	return err != nil
}

func (s *Server) cleanupDeadConnections() {
	s.logger.Debug("running cleanup dead connections")
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, tunnel := range s.tunnels {
		if s.isConnClosed(tunnel.WSConn) {
			s.logger.Info("removing dead tunnel connection", "id", id)
			delete(s.tunnels, id)

			// Clean up any pending requests for this tunnel
			for reqID, ch := range s.pendingRequests {
				if strings.HasPrefix(reqID, id) {
					close(ch)
					delete(s.pendingRequests, reqID)
				}
			}
		}
	}
}

// generateID generates a unique UUID for a tunnel
func generateID() string {
	return uuid.New().String()
}

// extractTunnelIDAndPath extracts the tunnel ID and the remaining path from a URL
func extractTunnelIDAndPath(urlStr string) (tunnelID string, remainingPath string, err error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", "", err
	}

	segments := strings.Split(strings.TrimPrefix(parsedURL.Path, "/"), "/")
	if len(segments) == 0 {
		return "", "", fmt.Errorf("no tunnel_id found in path")
	}

	// First segment is tunnel ID
	tunnelID = segments[0]
	if tunnelID == "" {
		return "", "", fmt.Errorf("empty tunnel_id")
	}

	// Anything after the tunnel ID is the remaining path
	if len(segments) > 1 {
		remainingPath = "/" + strings.Join(segments[1:], "/")
	}

	return tunnelID, remainingPath, nil
}
