package tunnel

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jwtly10/go-tunol/pkg/config"
	"golang.org/x/net/websocket"
)

type Server struct {
	addr            string
	tunnels         map[string]*Tunnel
	pendingRequests map[string]chan *HTTPResponse

	mu     sync.Mutex
	logger *slog.Logger
	cfg    *config.ServerConfig
}

type Tunnel struct {
	ID        string
	LocalPort int
	WSConn    *websocket.Conn
	Path      string // For local dev & pre-subdomain routing
	UrlPrefix string // For subdomain routing
	Created   time.Time
}

func NewServer(addr string, logger *slog.Logger, cfg *config.ServerConfig) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return &Server{
		addr:            addr,
		tunnels:         make(map[string]*Tunnel),
		pendingRequests: make(map[string]chan *HTTPResponse),

		logger: logger,
		cfg:    cfg,
	}
}

func (s *Server) Handler() http.Handler {
	return websocket.Handler(s.handleWS)
}

func (s *Server) handleWS(ws *websocket.Conn) {
	for {
		var msg Message
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			s.logger.Error("failed to recieve websocket message", "error", err)
			return
		}

		switch msg.Type {
		case MessageTypePing:
			s.logger.Info("received ping message")
			if err := websocket.JSON.Send(ws, Message{Type: MessageTypePong}); err != nil {
				s.logger.Error("failed to send websocket message", "error", err)
				return
			}

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
				ID:        id,
				LocalPort: req.LocalPort,
				WSConn:    ws,
				Path:      s.cfg.Scheme + "://" + s.cfg.Host + ":" + s.cfg.Port + "/" + id,
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

			s.logger.Info("new tunnel registered", "id", id, "local_port", req.LocalPort, "url", t.Path)

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
	s.logger.Info("received http request", "method", r.Method, "path", r.URL.Path)
	tunnelId := strings.TrimPrefix(r.URL.Path, "/")

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
		Path:      r.URL.Path,
		Body:      body,
		Headers:   headers,
		RequestId: requestId,
	}

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

// generateID generates a unique UUID for a tunnel
func generateID() string {
	return uuid.New().String()
}
