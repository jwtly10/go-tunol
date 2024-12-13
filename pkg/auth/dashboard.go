package auth

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"time"
)

type DashboardHandler struct {
	templates    *template.Template
	tokenService *TokenService
	logger       *slog.Logger
}

func NewDashboardHandler(templates *template.Template, tokenService *TokenService, logger *slog.Logger) *DashboardHandler {
	return &DashboardHandler{
		templates:    templates,
		tokenService: tokenService,
		logger:       logger,
	}
}

// HandleDashboard shows the main dashboard page
func (h *DashboardHandler) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*User)
	tokens, err := h.tokenService.ListUserTokens(user.ID)
	if err != nil {
		h.logger.Error("Failed to list tokens", "error", err)
		http.Error(w, "Failed to load tokens", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"User":   user,
		"Tokens": tokens,
	}

	h.logger.Info("Rendering dashboard",
		"userID", user.ID,
		"tokenCount", len(tokens))

	if err := h.templates.ExecuteTemplate(w, "layout.html", data); err != nil {
		h.logger.Error("Failed to render template", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// HandleCreateToken handles the creation of new API tokens
func (h *DashboardHandler) HandleCreateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := r.Context().Value("user").(*User)
	description := r.FormValue("description")
	if description == "" {
		http.Error(w, "Description is required", http.StatusBadRequest)
		return
	}

	token, err := h.tokenService.CreateToken(user.ID, description, 30*24*time.Hour) // 30 days validity
	if err != nil {
		h.logger.Error("Failed to create token", "error", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": token.PlainToken,
	})
}
