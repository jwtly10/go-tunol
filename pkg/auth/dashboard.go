package auth

import (
	"fmt"
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

	err = h.templates.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		h.logger.Error("Failed to render template", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
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

	data := map[string]interface{}{
		"User":  user,
		"Token": token,
	}

	err = h.templates.ExecuteTemplate(w, "token_created.html", data)
	if err != nil {
		h.logger.Error("Failed to render template", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
	}
}

// HandleRevokeToken handles revoking an existing token
func (h *DashboardHandler) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := r.Context().Value("user").(*User)
	tokenID := r.URL.Path[len("/dashboard/tokens/") : len(r.URL.Path)-len("/revoke")]

	err := h.tokenService.RevokeToken(tokenID, user.ID)
	if err != nil {
		h.logger.Error("Failed to revoke token", "error", err, "tokenID", tokenID)
		http.Error(w, "Failed to revoke token", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *TokenService) RevokeToken(tokenID string, userID int64) error {
	result, err := s.db.Exec(`
        UPDATE tokens 
        SET revoked_at = ? 
        WHERE id = ? AND user_id = ?
    `, time.Now(), tokenID, userID)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return fmt.Errorf("token not found or unauthorized")
	}

	return nil
}
