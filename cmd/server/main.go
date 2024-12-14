package main

import (
	"fmt"
	"github.com/jwtly10/go-tunol/pkg/auth"
	"github.com/jwtly10/go-tunol/pkg/database"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/jwtly10/go-tunol/pkg/config"
	"github.com/jwtly10/go-tunol/pkg/tunnel"
)

func setupLogger() *slog.Logger {
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				source := a.Value.Any().(*slog.Source)
				a.Value = slog.StringValue(source.File + ":" + strconv.Itoa(source.Line))
			}
			return a
		},
	}

	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}

func setupWebRoutes(mux *http.ServeMux, t *template.Template, authMiddleware *auth.AuthMiddleware, dashboardHandler *auth.DashboardHandler, authHandler *auth.AuthHandler) {
	// Public routes
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/terms", func(w http.ResponseWriter, r *http.Request) {
		if err := t.ExecuteTemplate(w, "terms", nil); err != nil {
			http.Error(w, "Failed to render page", http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/privacy", func(w http.ResponseWriter, r *http.Request) {
		if err := t.ExecuteTemplate(w, "privacy", nil); err != nil {
			http.Error(w, "Failed to render page", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/login", authHandler.HandleLogin)
	mux.HandleFunc("/auth/logout", authHandler.HandleLogout)
	mux.HandleFunc("/auth/validate", authHandler.HandleValidateToken)

	mux.HandleFunc("/auth/github/login", authHandler.HandleGitHubLogin)
	mux.HandleFunc("/auth/github/callback", authHandler.HandleGitHubCallback)

	// Protected routes
	mux.Handle("/dashboard", authMiddleware.RequireAuth(http.HandlerFunc(dashboardHandler.HandleDashboard)))
	mux.Handle("/dashboard/tokens", authMiddleware.RequireAuth(http.HandlerFunc(dashboardHandler.HandleCreateToken)))
}

func main() {
	logger := setupLogger()

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	if err := database.Initialize(cfg.Database); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	logger.Info("Database initialized")
	defer database.Close()
	db := database.GetDB()

	// Initialize services
	userRepo := auth.NewUserRepository(db)
	sessionService := auth.NewSessionService(db, logger)
	tokenService := auth.NewTokenService(db)

	// Load templates
	templates := template.Must(template.ParseGlob("templates/*.html"))

	// Initialize handlers
	authHandler := auth.NewAuthHandler(db, templates, tokenService, sessionService, userRepo, &cfg.Server, logger)
	authMiddleware := auth.NewAuthMiddleware(sessionService, userRepo, logger)
	dashboardHandler := auth.NewDashboardHandler(templates, tokenService, logger)

	// Initialize tunnel server
	tunnelServer := tunnel.NewServer(tokenService, logger, &cfg.Server)

	// Setup mux and routes
	mux := http.NewServeMux()
	setupWebRoutes(mux, templates, authMiddleware, dashboardHandler, authHandler)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Just so we can have a nice landing page on root URL
		if r.URL.Path == "/" {
			logger.Info("redirecting root request to login")
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}
		logger.Debug("handling tunnel proxy request",
			"path", r.URL.Path,
			"method", r.Method,
			"remote_addr", r.RemoteAddr)

		// For all other paths (like /abc123/), try to handle as tunnel request
		// the server will handle the request as needed
		tunnelServer.ServeHTTP(w, r)
	})

	// Handle tunnel requests
	mux.HandleFunc("/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			logger.Warn("non-websocket request to /tunnel endpoint",
				"remote_addr", r.RemoteAddr,
				"method", r.Method)
			http.Error(w, "Expected WebSocket connection", http.StatusBadRequest)
			return
		}
		tunnelServer.Handler().ServeHTTP(w, r)
	})

	// Start server
	port := ":" + cfg.Server.Port
	logger.Info(fmt.Sprintf("Server listening on %s", port))
	if err := http.ListenAndServe(port, mux); err != nil {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
}
