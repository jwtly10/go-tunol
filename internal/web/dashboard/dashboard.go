package dashboard

import (
	"encoding/json"
	"github.com/jwtly10/go-tunol/internal/auth/token"
	"github.com/jwtly10/go-tunol/internal/web/user"
	"html/template"
	"log/slog"
	"net/http"
	"time"
)

type Handler struct {
	templates    *template.Template
	tokenService *token.Service
	logger       *slog.Logger
}

func NewDashboardHandler(templates *template.Template, tokenService *token.Service, logger *slog.Logger) *Handler {
	return &Handler{
		templates:    templates,
		tokenService: tokenService,
		logger:       logger,
	}
}

// HandleDashboard shows the main dashboard page
func (h *Handler) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value("user").(*user.User)
	tokens, err := h.tokenService.ListUserTokens(u.ID)
	if err != nil {
		h.logger.Error("Failed to list tokens", "error", err)
		http.Error(w, "Failed to load tokens", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"User":   u,
		"Tokens": tokens,
	}

	h.logger.Info("Rendering dashboard",
		"userID", u.ID,
		"tokenCount", len(tokens))

	if err := h.templates.ExecuteTemplate(w, "layout.html", data); err != nil {
		h.logger.Error("Failed to render template", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// HandleCreateToken handles the creation of new API tokens
func (h *Handler) HandleCreateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u := r.Context().Value("user").(*user.User)
	description := r.FormValue("description")
	if description == "" {
		http.Error(w, "Description is required", http.StatusBadRequest)
		return
	}

	t, err := h.tokenService.CreateToken(u.ID, description, 30*24*time.Hour) // 30 days validity
	if err != nil {
		h.logger.Error("Failed to create token", "error", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": t.PlainToken,
	})
}
