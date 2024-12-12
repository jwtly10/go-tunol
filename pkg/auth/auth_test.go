package auth

import (
	"database/sql"
	"github.com/jwtly10/go-tunol/pkg/config"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"html/template"
	"io/ioutil"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *sql.DB {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	// Read and execute all migrations
	migrationsDir := "../../pkg/db/migrations"
	files, err := ioutil.ReadDir(migrationsDir)
	require.NoError(t, err)

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".sql" {
			continue
		}

		migrationPath := filepath.Join(migrationsDir, file.Name())
		migrationSQL, err := ioutil.ReadFile(migrationPath)
		require.NoError(t, err)

		_, err = db.Exec(string(migrationSQL))
		require.NoError(t, err, "failed to execute migration: %s", file.Name())
	}

	return db
}

func setupTestAuth(t *testing.T) (*AuthHandler, *sql.DB) {
	db := setupTestDB(t)

	// Load templates
	tmpl, err := template.ParseGlob("../../templates/*.html")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		Auth: config.AuthConfig{
			GithubClientId:     "test-client-id",
			GithubClientSecret: "test-client",
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	tokenService := NewTokenService(db)
	userRepo := NewUserRepository(db)
	sessionService := NewSessionService(db, logger)

	auth := NewAuthHandler(db, tmpl, tokenService, sessionService, userRepo, cfg, logger)
	return auth, db
}

func TestTokenValidation(t *testing.T) {
	auth, db := setupTestAuth(t)

	// First create a test user
	userID := 1
	_, err := db.Exec(
		"INSERT INTO users (id, github_id, github_username) VALUES (?, ?, ?)",
		userID, 12345, "testuser",
	)
	require.NoError(t, err)

	// Create a valid token
	testToken := "test-token-123"
	tokenHash := HashToken(testToken)
	validExpiryTime := time.Now().Add(24 * time.Hour)
	_, err = db.Exec(
		`INSERT INTO tokens (user_id, token_hash, expires_at) 
         VALUES (?, ?, ?)`,
		userID, tokenHash, validExpiryTime,
	)
	require.NoError(t, err)

	// Create an expired token
	expiredToken := "expired-token-123"
	expiredTokenHash := HashToken(expiredToken)
	expiredTime := time.Now().Add(-24 * time.Hour)
	_, err = db.Exec(
		`INSERT INTO tokens (user_id, token_hash, expires_at) 
         VALUES (?, ?, ?)`,
		userID, expiredTokenHash, expiredTime,
	)
	require.NoError(t, err)

	tests := []struct {
		name       string
		token      string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid token",
			token:      testToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "expired token",
			token:      expiredToken,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "Invalid or expired token",
		},
		{
			name:       "invalid token",
			token:      "invalid-token",
			wantStatus: http.StatusUnauthorized,
			wantBody:   "Invalid or expired token",
		},
		{
			name:       "missing token",
			token:      "",
			wantStatus: http.StatusUnauthorized,
			wantBody:   "No token provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/auth/validate", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			w := httptest.NewRecorder()

			auth.ValidateClientToken(w, req)

			require.Equal(t, tt.wantStatus, w.Code)
			if tt.wantBody != "" {
				require.Contains(t, w.Body.String(), tt.wantBody)
			}
		})
	}
}
