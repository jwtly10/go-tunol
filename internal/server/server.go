package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/jwtly10/go-tunol/internal/config"
)

type Server struct {
	tunnel *TunnelHandler
	web    *WebHandler
	logger *slog.Logger

	cfg *config.ServerConfig
}

func NewServer(tunnel *TunnelHandler, web *WebHandler, logger *slog.Logger, cfg *config.ServerConfig) *Server {
	return &Server{
		tunnel: tunnel,
		web:    web,
		logger: logger,
		cfg:    cfg,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/tunnel" && r.Header.Get("Upgrade") == "websocket" {
		s.tunnel.HandleWS().ServeHTTP(w, r)
		return
	}

	// HACK
	// This is a bit of a hack to get around localhost subdomain requirements
	// Locally all tunnel requests will go through like
	// http://localhost:8001/local/tunnelID/externalpath
	// But on prod this will look more like
	// https://tunnelID.tunol.dev/externalpath

	if s.cfg.UseSubdomains {
		parts := strings.Split(r.Host, ".")

		// Check if we have a subdomain (more than 2 parts and not "www")
		if len(parts) > 2 && parts[0] != "www" {
			s.tunnel.ServeHTTP(w, r)
			return
		}
	}

	if strings.HasPrefix(r.URL.Path, "/local/") {
		s.tunnel.ServeHTTP(w, r)
		return
	}

	s.web.ServeHTTP(w, r)
}
