// web.go
package server

import (
	"html/template"
	"log/slog"
	"net/http"

	"github.com/jwtly10/go-tunol/internal/web/auth"
	"github.com/jwtly10/go-tunol/internal/web/dashboard"
)

type WebHandler struct {
	mux            *http.ServeMux
	templates      *template.Template
	authMiddleware *auth.Middleware
	logger         *slog.Logger
}

func NewWebHandler(
	templates *template.Template,
	authMiddleware *auth.Middleware,
	dashboardHandler *dashboard.Handler,
	authHandler *auth.Handler,
	logger *slog.Logger,
) *WebHandler {
	mux := http.NewServeMux()

	// Redirect root to login
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
	})

	// Public routes
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/terms", func(w http.ResponseWriter, r *http.Request) {
		if err := templates.ExecuteTemplate(w, "terms", nil); err != nil {
			http.Error(w, "Failed to render page", http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/privacy", func(w http.ResponseWriter, r *http.Request) {
		if err := templates.ExecuteTemplate(w, "privacy", nil); err != nil {
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

	return &WebHandler{
		mux:            mux,
		templates:      templates,
		authMiddleware: authMiddleware,
		logger:         logger,
	}
}

func (h *WebHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
