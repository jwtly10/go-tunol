package auth

import (
	"github.com/jwtly10/go-tunol/internal/web/user"
	"golang.org/x/net/context"
	"log/slog"
	"net/http"
	"time"
)

const (
	sessionCookie   = "tunol_session"
	cookiePath      = "/"
	sessionDuration = 30 * time.Minute
)

type Middleware struct {
	sessionService *SessionService
	userRepository *user.Repository
	logger         *slog.Logger
}

func NewAuthMiddleware(sessionService *SessionService, userRepository *user.Repository, logger *slog.Logger) *Middleware {
	return &Middleware{
		sessionService: sessionService,
		userRepository: userRepository,
		logger:         logger,
	}
}

func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			m.logger.Error("Failed to fetch session cookie", "error", err)
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		session, err := m.sessionService.ValidateSession(cookie.Value)
		if err != nil {
			m.logger.Error("Failed to validate session", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if session == nil {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		// Add session and user to request context
		user, err := m.userRepository.FindByID(session.UserID)
		if err != nil {
			m.logger.Error("Failed to fetch user", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		m.logger.Info("User authenticated", "user", user.GithubUsername)

		ctx := context.WithValue(r.Context(), "user", user)
		ctx = context.WithValue(ctx, "session", session)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
