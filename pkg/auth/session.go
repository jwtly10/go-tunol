package auth

import (
	"database/sql"
	"github.com/google/uuid"
	"log/slog"
	"time"
)

type Session struct {
	ID        int64
	UserID    int64
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type SessionService struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewSessionService(db *sql.DB, logger *slog.Logger) *SessionService {
	return &SessionService{
		db:     db,
		logger: logger,
	}
}

func (s *SessionService) CreateSession(userID int64, duration time.Duration) (*Session, error) {
	session := &Session{
		UserID:    userID,
		Token:     uuid.New().String(),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(duration),
	}

	result, err := s.db.Exec(`
        INSERT INTO sessions (user_id, token, created_at, expires_at)
        VALUES (?, ?, ?, ?)
    `, session.UserID, session.Token, session.CreatedAt, session.ExpiresAt)

	if err != nil {
		return nil, err
	}

	// Get the auto-generated ID
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	session.ID = id

	return session, nil
}

func (s *SessionService) ValidateSession(token string) (*Session, error) {
	session := &Session{}
	err := s.db.QueryRow(`
        SELECT id, user_id, token, created_at, expires_at
        FROM sessions
        WHERE token = ? AND expires_at > ?
    `, token, time.Now()).Scan(
		&session.ID,
		&session.UserID,
		&session.Token,
		&session.CreatedAt,
		&session.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (s *SessionService) DeleteSession(token string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}
