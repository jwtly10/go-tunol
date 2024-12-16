package token

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/jwtly10/go-tunol/internal/db"
	"github.com/jwtly10/go-tunol/internal/utils"
	"time"

	"github.com/google/uuid"
)

type Token struct {
	ID          int64
	UserId      int64
	Hash        string
	PlainToken  string // Reminder, this is just to temporarily store the plain token, so we can return it to user
	Description string
	LastUsed    *time.Time // May be nil if never used
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time // May be nil if not revoked
}

func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

type Service struct {
	db *db.Database
}

func NewTokenService(db *db.Database) *Service {
	return &Service{db: db}
}

func (s *Service) CreateToken(userId int64, description string, validity time.Duration) (*Token, error) {
	plainToken := uuid.New().String() + "-" + uuid.New().String()

	hash := utils.HashToken(plainToken)

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

	// If we successfully created a token, we should revoke all other tokens for this user
	// as the user can only have one at a time
	_, err = s.db.Exec(`UPDATE tokens SET revoked_at = ? WHERE user_id = ? AND id != ?`, time.Now(), userId, id)
	if err != nil {
		return nil, err
	}

	return token, nil
}

func (s *Service) ValidateToken(plainToken string) (bool, error) {
	hash := utils.HashToken(plainToken)
	var token Token
	if err := s.db.QueryRow(`SELECT
    id, user_id, token_hash, description, last_used, created_at, expires_at, revoked_at
	FROM tokens WHERE token_hash = ?`, hash).Scan(
		&token.ID,
		&token.UserId,
		&token.Hash,
		&token.Description,
		&token.LastUsed,
		&token.CreatedAt,
		&token.ExpiresAt,
		&token.RevokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("token does not exist")
		}
	}

	if token.RevokedAt != nil {
		return false, fmt.Errorf("token has been revoked")
	}

	// Set as revoked if expired
	if time.Now().After(token.ExpiresAt) {
		_, err := s.db.Exec(`UPDATE tokens SET revoked_at = ? WHERE id = ?`, time.Now(), token.ID)
		if err != nil {
			return false, fmt.Errorf("failed to revoke token: %w", err)
		}
		return false, fmt.Errorf("token has expired")
	}

	// Set last used
	_, err := s.db.Exec(`UPDATE tokens SET last_used = ? WHERE id = ?`, time.Now(), token.ID)
	if err != nil {
		return false, fmt.Errorf("failed to update last used: %w", err)
	}

	return true, nil
}

func (s *Service) ListUserTokens(userID int64) ([]Token, error) {
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
			t.LastUsed = &lastUsed.Time
		}
		if revokedAt.Valid {
			t.RevokedAt = &revokedAt.Time
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}
