package server

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jwtly10/go-tunol/internal/proto"
	"golang.org/x/net/websocket"
)

func (th *TunnelHandler) handleWS(ws *websocket.Conn) {
	defer func() {
		th.mu.Lock()
		// Clean up all tunnels associated with this connection
		for id, tunnel := range th.tunnels {
			if tunnel.WSConn == ws {
				th.logger.Info("cleaning up disconnected tunnel", "id", id, "total", len(th.tunnels)-1)
				tunnel.WSConn.Close()
				delete(th.tunnels, id)

				// Clean up any pending requests for this tunnel
				for reqID, ch := range th.pendingRequests {
					if strings.HasPrefix(reqID, id) {
						close(ch)
						delete(th.pendingRequests, reqID)
					}
				}
			}
		}
		th.mu.Unlock()
	}()

	for {
		var msg proto.Message
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			var id string
			for _, tunnel := range th.tunnels {
				if tunnel.WSConn == ws {
					id = tunnel.ID
				}
			}
			if err == io.EOF {
				th.logger.Info("client disconnected", "id", id, "error", err)
			} else {
				th.logger.Info("websocket error", "id", id, "error", err)
			}
			return // Trigger deferred clean up
		}

		if msg.Type == proto.MessageTypeHTTPResponse {
			var resp proto.HTTPResponse
			if b, err := json.Marshal(msg.Payload); err == nil {
				if err := json.Unmarshal(b, &resp); err == nil {
					th.mu.Lock()
					for _, tunnel := range th.tunnels {
						if tunnel.WSConn == ws {
							tunnel.LastActivity = time.Now()
						}
					}
					th.mu.Unlock()
				}
			}
		}

		switch msg.Type {
		case proto.MessageTypePing:
			th.logger.Info("received ping message")
			if err := websocket.JSON.Send(ws, proto.Message{Type: proto.MessageTypePong}); err != nil {
				th.logger.Error("failed to send websocket message", "error", err)
				return
			}

		case proto.MessageTypePong:
			th.logger.Info("received pong message")

		case proto.MessageTypeTunnelReq:
			th.logger.Info("received tunnel request", "payload", msg.Payload)
			var req proto.TunnelRequest
			b, err := json.Marshal(msg.Payload)
			if err != nil {
				th.logger.Error("failed to marshal tunnel request", "error", err)
				return
			}

			if err := json.Unmarshal(b, &req); err != nil {
				th.logger.Error("failed to unmarshal tunnel request", "error", err)
			}

			id := generateID()

			t := &Tunnel{
				ID:           id,
				LocalPort:    req.LocalPort,
				WSConn:       ws,
				Path:         th.cfg.SubdomainURL(id),
				LastActivity: time.Now(),
				Created:      time.Now(),
			}

			th.mu.Lock()
			th.tunnels[id] = t
			th.mu.Unlock()

			resp := proto.Message{
				Type: proto.MessageTypeTunnelResp,
				Payload: proto.TunnelResponse{
					URL: t.Path,
				},
			}

			if err := websocket.JSON.Send(ws, resp); err != nil {
				th.logger.Error("failed to send tunnel response", "error", err)
			}

			th.logger.Info("new tunnel registered", "totalTunnels", len(th.tunnels), "id", id, "localPort", req.LocalPort, "url", t.Path)

		case proto.MessageTypeHTTPResponse:
			th.logger.Info("received http response from tunnel", "payload", msg.Payload)
			var resp proto.HTTPResponse
			b, _ := json.Marshal(msg.Payload)
			if err := json.Unmarshal(b, &resp); err != nil {
				th.logger.Error("failed to unmarshal HTTP response", "error", err)
				continue
			}

			th.mu.Lock()
			if ch, exists := th.pendingRequests[resp.RequestId]; exists {
				ch <- &resp
				delete(th.pendingRequests, resp.RequestId)
			}
			th.mu.Unlock()

		default:
			th.logger.Warn("unknown message type", "type", msg.Type, "content", msg)
		}
	}
}

// authenticateWebSocket verifies the token during WebSocket upgrade
func (th *TunnelHandler) authenticateWebSocket(ws *websocket.Conn) error {
	token := ""
	if ws.Request() != nil {
		token = th.extractToken(ws.Request())
	}

	if token == "" {
		return fmt.Errorf("no token provided")
	}

	valid, err := th.tokenService.ValidateToken(token)
	// We will never have an error without valid being false, so we can just handle that as is
	// So if false, theres a specific error we may need to handle
	if !valid {
		return fmt.Errorf("invalid token: %v", err)
	}

	return nil
}

// cleanupLoop periodically checks for dead connections and cleans them up
func (th *TunnelHandler) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			th.cleanupDeadConnections()
		case <-th.done:
			return
		}
	}
}

// isConnClosed pings the websocket connection to check if it's still alive
func (th *TunnelHandler) isConnClosed(ws *websocket.Conn) bool {
	err := websocket.JSON.Send(ws, proto.Message{Type: proto.MessageTypePing})
	return err != nil
}

// cleanupDeadConnections removes any tunnels with closed connections
func (th *TunnelHandler) cleanupDeadConnections() {
	th.logger.Debug("running cleanup dead connections")
	th.mu.Lock()
	defer th.mu.Unlock()

	for id, tunnel := range th.tunnels {
		if th.isConnClosed(tunnel.WSConn) {
			th.logger.Info("removing dead tunnel connection", "id", id)
			delete(th.tunnels, id)

			// Clean up any pending requests for this tunnel
			for reqID, ch := range th.pendingRequests {
				if strings.HasPrefix(reqID, id) {
					close(ch)
					delete(th.pendingRequests, reqID)
				}
			}
		}
	}
}
