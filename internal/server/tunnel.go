package server

import (
	"bytes"
	"compress/gzip"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/config"
	"github.com/jwtly10/go-tunol/internal/proto"
	"golang.org/x/net/websocket"
)

type TunnelHandler struct {
	tunnels         map[string]*Tunnel
	pendingRequests map[string]chan *proto.HTTPResponse
	tokenService    *token.Service
	templates       *template.Template

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

func NewTunnelHandler(tokenService *token.Service, templates *template.Template, logger *slog.Logger, cfg *config.ServerConfig) *TunnelHandler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	th := &TunnelHandler{
		tunnels:         make(map[string]*Tunnel),
		pendingRequests: make(map[string]chan *proto.HTTPResponse),
		tokenService:    tokenService,
		templates:       templates,

		logger: logger,
		cfg:    cfg,
		done:   make(chan struct{}),
	}

	go th.cleanupLoop()

	return th
}

// HandleWS handles incoming websocket connections from the client
func (th *TunnelHandler) HandleWS() http.Handler {
	return websocket.Handler(func(ws *websocket.Conn) {
		// Authenticate WebSocket connection
		if err := th.authenticateWebSocket(ws); err != nil {
			th.logger.Error("websocket authentication failed", "error", err)
			// Send error message before closing
			errMsg := proto.Message{
				Type: proto.MessageTypeError,
				Payload: map[string]string{
					"error": err.Error(),
				},
			}
			err := websocket.JSON.Send(ws, errMsg)
			if err != nil {
				th.logger.Error("failed to send error message", "error", err)
			}
			ws.Close()
			return
		}

		th.handleWS(ws)
	})
}

// ServeHTTP handles incoming HTTP tunnel requests from the client, for proxying to the CLI tunnel
func (th *TunnelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	th.logger.Info("1. initial request proxied from cloudflare", "headers", r.Header)
	th.logger.Info("received http request", "method", r.Method, "rawPath", r.URL.Path)

	// https://tunelID.tunol.dev/some_external_path/and/maybe/more
	// http://localhost:8001/local/tunnelID/some_external_path/and/maybe/more
	tunnelId, realPath, err := extractTunnelIDAndPath(r.URL.String(), r.Host, th.cfg.UseSubdomains)
	if err != nil {
		th.logger.Error("failed to extract tunnel_id from url", "error", err)
	}

	th.mu.Lock()
	tunnel, exists := th.tunnels[tunnelId]
	th.mu.Unlock()

	if !exists {
		th.logger.Warn("tunnel not found", "id", tunnelId)
		w.WriteHeader(http.StatusNotFound)

		// nicer formatting of content for the template
		var data map[string]interface{}
		if th.cfg.UseSubdomains {
			// x8q4j44y.tunol.dev/
			data = map[string]interface{}{
				"TunnelUrl": r.Host,
			}
		} else {
			// localhost:8001/local/fjkdsfs  or
			data = map[string]interface{}{
				"TunnelUrl": r.Host + "/local/" + tunnelId,
			}
		}

		if err := th.templates.ExecuteTemplate(w, "tunnel-not-found", data); err != nil {
			th.logger.Error("failed to render not found template", "error", err)
		}
		return
	}

	// We need to be able to wait for the response from the CLI tunnel
	respChan := make(chan *proto.HTTPResponse, 1)
	requestId := generateID()

	th.mu.Lock()
	th.pendingRequests[requestId] = respChan
	th.mu.Unlock()

	// Clean up the pending request once done
	defer func() {
		th.mu.Lock()
		delete(th.pendingRequests, requestId)
		th.mu.Unlock()
	}()

	// Map the HTTP request to a WS message
	th.logger.Info("initial request headers", "headers", r.Header)
	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = v[0]
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		th.logger.Error("failed to read request body", "error", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	httpReq := proto.HTTPRequest{
		Method:    r.Method,
		Path:      realPath,
		Body:      body,
		Headers:   headers,
		RequestId: requestId,
	}

	msg := proto.Message{
		Type:    proto.MessageTypeHTTPRequest,
		Payload: httpReq,
	}

	th.logger.Info("fowarding http request to tunel ",
		"tunnel_id", tunnelId,
		"headers", httpReq.Headers,
		"method", r.Method,
		"path", realPath,
		"requestId", requestId)

	th.logger.Info("2. sending through websocket", "headers", httpReq.Headers)

	if err := websocket.JSON.Send(tunnel.WSConn, msg); err != nil {
		http.Error(w, "Failed to forward request", http.StatusInternalServerError)
		return
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		th.logger.Info("received response through tunnel",
			"requestId", requestId,
			"statusCode", resp.StatusCode,
			"responseHeaders", resp.Headers)

		// Similar to client
		// We, need to clean up any headers that may conflict with cloudflare

		isWebSocketUpgrade := strings.EqualFold(resp.Headers["Upgrade"], "websocket") &&
			strings.EqualFold(resp.Headers["Connection"], "upgrade")

		// Headers to keep
		var responseHeadersToKeep = map[string]bool{
			"content-type":   true,
			"content-length": true,
			"set-cookie":     true,
			"location":       true,
			"cache-control":  true,
			"expires":        true,
			"etag":           true,
			"last-modified":  true,
			"vary":           true,
			"x-request-id":   true,
			"date":           true,
			"server":         true,
			"authorization":  true,
		}

		// Add WebSocket specific headers if needed
		if isWebSocketUpgrade {
			responseHeadersToKeep["connection"] = true
			responseHeadersToKeep["upgrade"] = true
			responseHeadersToKeep["sec-websocket-key"] = true
			responseHeadersToKeep["sec-websocket-version"] = true
			responseHeadersToKeep["sec-websocket-protocol"] = true
			responseHeadersToKeep["sec-websocket-extensions"] = true
		}

		cleaned := make(map[string]string)
		for k, v := range resp.Headers {
			headerLower := strings.ToLower(k)
			if responseHeadersToKeep[headerLower] {
				cleaned[k] = v
			}
		}

		// We also need to handle gzipped responses
		if isGzipped(resp.Headers) {
			delete(cleaned, "Content-Encoding")

			reader, err := gzip.NewReader(bytes.NewReader(resp.Body))
			if err != nil {
				th.logger.Error("failed to create gzip reader", "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			defer reader.Close()

			// Read the uncompressed content
			uncompressedBody, err := io.ReadAll(reader)
			if err != nil {
				th.logger.Error("failed to read gzipped content", "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			for k, v := range cleaned {
				w.Header().Set(k, v)
			}

			th.logger.Info("final response details",
				"status_code", resp.StatusCode,
				"is_redirect", resp.StatusCode >= 300 && resp.StatusCode < 400,
				"final_location", w.Header().Get("Location"),
				"all_headers", w.Header(),
				"original_headers", resp.Headers,
				"request_id", requestId)

			th.logger.Info("7. this is what cloudflare gets on the other end", "headers", cleaned)

			w.WriteHeader(resp.StatusCode)
			w.Write(uncompressedBody)

			th.logger.Info("handled gzipped response")
			return

		}
		// Else handle non-gzipped response

		for k, v := range cleaned {
			w.Header().Set(k, v)
		}

		th.logger.Info("7. this is what cloudflare gets on the other end", "headers", cleaned)
		w.WriteHeader(resp.StatusCode)

		w.Write(resp.Body)

	case <-time.After(30 * time.Second): // TODO: Make this some sort of configurable timeout

		http.Error(w, "Request timed out", http.StatusGatewayTimeout)
	}
}

// Shutdown provides a way to gracefully shutdown the server
func (th *TunnelHandler) Shutdown() {
	close(th.done)
	th.cleanupDeadConnections()
}

func isGzipped(headers map[string]string) bool {
	return strings.Contains(strings.ToLower(headers["Content-Encoding"]), "gzip")
}
