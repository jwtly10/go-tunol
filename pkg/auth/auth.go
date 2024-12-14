package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jwtly10/go-tunol/pkg/config"
	_ "github.com/mattn/go-sqlite3"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type AuthHandler struct {
	db             *sql.DB
	templates      *template.Template
	tokenService   *TokenService
	sessionService *SessionService
	userRepository *UserRepository
	cfg            *config.ServerConfig
	logger         *slog.Logger
}

func NewAuthHandler(db *sql.DB, tmpl *template.Template, tokenService *TokenService, sessionService *SessionService, userRepository *UserRepository, cfg *config.ServerConfig, logger *slog.Logger) *AuthHandler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	return &AuthHandler{
		db:             db,
		templates:      tmpl,
		tokenService:   tokenService,
		sessionService: sessionService,
		userRepository: userRepository,
		cfg:            cfg,
		logger:         logger,
	}
}

// HandleLogin shows the login page
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// Check if user is already authenticated
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		session, err := h.sessionService.ValidateSession(cookie.Value)
		if err == nil && session != nil {
			// User is already logged in, redirect to dashboard
			http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
			return
		}
	}

	err = h.templates.ExecuteTemplate(w, "login.html", nil)
	if err != nil {
		h.logger.Error("Failed to render login template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// ValidateClientToken is for validation of any client HTTP requests have a valid token
func (h *AuthHandler) ValidateClientToken(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "No token provided", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	valid, err := h.tokenService.ValidateToken(token)
	if err != nil {
		h.logger.Error("Failed to validate token", "error", err)
		http.Error(w, "Failed to validate token", http.StatusInternalServerError)
		return
	}

	if !valid {
		http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GitHub oAuth flow
const (
	githubTokenURL = "https://github.com/login/oauth/access_token"
	githubUserURL  = "https://api.github.com/user"
)

type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	Email     string `json:"email"`
}

func (h *AuthHandler) HandleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	state := uuid.New().String()

	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&state=%s&scope=user:email",
		h.cfg.Auth.GithubClientId,
		state,
	)

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (h *AuthHandler) HandleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Error(w, "Missing code or state", http.StatusBadRequest)
		return
	}

	// TODO: Validate state matches what we stored in cookie/session

	// Exchange code for GitHub access token
	accessToken, err := h.exchangeCodeForToken(code)
	if err != nil {
		h.logger.Error("Failed to exchange code for token", "error", err)
		http.Error(w, "Authentication failed", http.StatusInternalServerError)
		return
	}

	// Fetch GitHub user info
	githubUser, err := h.fetchGitHubUser(accessToken)
	if err != nil {
		h.logger.Error("Failed to fetch GitHub user", "error", err)
		http.Error(w, "Failed to fetch user information", http.StatusInternalServerError)
		return
	}

	user := &User{
		GithubID:        githubUser.ID,
		GithubUsername:  githubUser.Login,
		GithubAvatarURL: githubUser.AvatarURL,
		GithubEmail:     githubUser.Email,
	}

	// Create or update user
	user, err = h.userRepository.CreateOrUpdateUser(user)
	if err != nil {
		h.logger.Error("Failed to create/update user", "error", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Create a new session
	session, err := h.sessionService.CreateSession(user.ID, sessionDuration)
	if err != nil {
		h.logger.Error("Failed to create session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set http only session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    session.Token,
		Path:     cookiePath,
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
}

func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		err = h.sessionService.DeleteSession(cookie.Value)
		if err != nil {
			h.logger.Error("Failed to delete session", "error", err)
		}
	}

	h.logger.Info("User logged out")

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     cookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
}

func (h *AuthHandler) exchangeCodeForToken(code string) (string, error) {
	data := url.Values{
		"client_id":     {h.cfg.Auth.GithubClientId},
		"client_secret": {h.cfg.Auth.GithubClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequest("POST", githubTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.AccessToken, nil
}

func (h *AuthHandler) fetchGitHubUser(accessToken string) (*githubUser, error) {
	req, err := http.NewRequest("GET", githubUserURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "token "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (h *AuthHandler) HandleValidateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the authorisation header and validate
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "No token provided", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	_, err := h.tokenService.ValidateToken(token)
	if err != nil {
		h.logger.Error("Failed to validate token", "error", err)
		http.Error(w, fmt.Sprintf("Invalid token: %s", err), http.StatusInternalServerError)
		return
	}
}
