package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/db"
	"github.com/jwtly10/go-tunol/internal/server/middleware"
	"github.com/jwtly10/go-tunol/internal/web/auth"
	"github.com/jwtly10/go-tunol/internal/web/dashboard"
	_ "github.com/jwtly10/go-tunol/internal/web/dashboard"
	"github.com/jwtly10/go-tunol/internal/web/user"

	"github.com/jwtly10/go-tunol/internal/config"
	"github.com/jwtly10/go-tunol/internal/server"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
		os.Exit(1)
	}

	logger := cfg.Server.Logger

	d, err := db.Initialize(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	logger.Info("Database initialized")
	defer d.Close()

	// Initialize services
	userRepo := user.NewUserRepository(d)
	sessionService := auth.NewSessionService(d, logger)
	tokenService := token.NewTokenService(d)

	// Load templates
	templates := template.Must(template.ParseGlob("templates/*.html"))

	// Initialize handlers
	authHandler := auth.NewAuthHandler(d, templates, tokenService, sessionService, userRepo, &cfg.Server, logger)
	authMiddleware := auth.NewAuthMiddleware(sessionService, userRepo, logger)
	dashboardHandler := dashboard.NewDashboardHandler(templates, tokenService, logger)

	// Initialize handlers
	tunnelHandler := server.NewTunnelHandler(tokenService, logger, &cfg.Server)
	webHandler := server.NewWebHandler(templates, authMiddleware, dashboardHandler, authHandler, logger)

	// Initialize server
	server := server.NewServer(tunnelHandler, webHandler, logger, &cfg.Server)

	// Wrap with middleware
	loggingHandler := middleware.WithLogging(server, logger)

	// Start server
	port := ":" + cfg.Server.Port
	logger.Info(fmt.Sprintf("Server listening on %s", port))
	if err := http.ListenAndServe(port, loggingHandler); err != nil {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
}
