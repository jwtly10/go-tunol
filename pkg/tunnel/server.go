package tunnel

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jwtly10/go-tunol/pkg/config"
	"golang.org/x/net/websocket"
)

type Server struct {
	addr    string
	tunnels map[string]*Tunnel

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
		addr:    addr,
		tunnels: make(map[string]*Tunnel),
		logger:  logger,
		cfg:     cfg,
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
			if err := websocket.JSON.Send(ws, Message{Type: MessageTypePong}); err != nil {
				s.logger.Error("failed to send websocket message", "error", err)
				return
			}

		case MessageTypeTunnelReq:
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

		default:
			s.logger.Warn("unknown message type", "type", msg.Type, "content", msg)
		}
	}
}

// generateID generates a unique UUID for a tunnel
func generateID() string {
	return uuid.New().String()
}

// generatePathSuffix generates a random string to append to the tunnel path
func generatePathSuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 10)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
