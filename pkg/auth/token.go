package auth

import (
	"database/sql"
	"github.com/google/uuid"
	"time"
)

type Token struct {
	ID          int64
	UserId      int64
	Hash        string
	PlainToken  string // Reminder, this is just to temporarily store the plain token, so we can return it to user
	Description string
	LastUsed    time.Time
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

type TokenService struct {
	db *sql.DB
}

func NewTokenService(db *sql.DB) *TokenService {
	return &TokenService{db: db}
}

func (s *TokenService) CreateToken(userId int64, description string, validity time.Duration) (*Token, error) {
	plainToken := uuid.New().String() + "-" + uuid.New().String()

	hash := HashToken(plainToken)

	token := &Token{
		UserId:      userId,
		Hash:        hash,
		PlainToken:  plainToken,
		Description: description,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(validity),
	}

	result, err := s.db.Exec(`
        INSERT INTO tokens (user_id, token_hash, description, created_at, expires_at)
        VALUES (?, ?, ?, ?, ?)
    `, token.UserId, token.Hash, token.Description, token.CreatedAt, token.ExpiresAt)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	token.ID = id

	return token, nil
}

func (s *TokenService) ValidateToken(plainToken string) (bool, error) {
	hash := HashToken(plainToken)

	var expiresAt time.Time
	err := s.db.QueryRow(`
        SELECT expires_at FROM tokens 
        WHERE token_hash = ? AND revoked_at IS NULL
    `, hash).Scan(&expiresAt)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return time.Now().Before(expiresAt), nil
}

func (s *TokenService) ListUserTokens(userID int64) ([]Token, error) {
	rows, err := s.db.Query(`
        SELECT id, user_id, token_hash, description, last_used, created_at, expires_at, revoked_at
        FROM tokens
        WHERE user_id = ?
        ORDER BY created_at DESC
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		var lastUsed, revokedAt sql.NullTime
		err := rows.Scan(&t.ID, &t.UserId, &t.Hash, &t.Description, &lastUsed, &t.CreatedAt, &t.ExpiresAt, &revokedAt)
		if err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			t.LastUsed = lastUsed.Time
		}
		if revokedAt.Valid {
			t.RevokedAt = &revokedAt.Time
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}
