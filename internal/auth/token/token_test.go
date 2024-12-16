package token

import (
	testutil "github.com/jwtly10/go-tunol/internal/testutils"
	"github.com/jwtly10/go-tunol/internal/web/user"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestTokenGeneration(t *testing.T) {
	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	tokenService := NewTokenService(db)
	userRepo := user.NewUserRepository(db)

	// Create test user
	user := &user.User{
		ID:              0,
		GithubID:        12345,
		GithubUsername:  "testuser",
		GithubAvatarURL: "https://github.com/avatar.jpg",
		GithubEmail:     "test@example.com",
	}

	user, err := userRepo.CreateUser(user)
	require.NoError(t, err)

	token, err := tokenService.CreateToken(user.ID, "Test token", 24*time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, token.PlainToken)
	require.NotEmpty(t, token.Hash)
	// Now validate it
	valid, err := tokenService.ValidateToken(token.PlainToken)
	require.NoError(t, err)
	require.True(t, valid)

	// Test expired token
	expiredToken, err := tokenService.CreateToken(user.ID, "Expired token", -1*time.Hour)
	require.NoError(t, err)

	// Test validate token
	valid, err = tokenService.ValidateToken(expiredToken.PlainToken)
	require.Error(t, err)
}

func TestListUserTokens(t *testing.T) {
	// Init basic test environment
	db, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	tokenService := NewTokenService(db)
	userRepo := user.NewUserRepository(db)

	user := &user.User{
		ID:             1,
		GithubID:       12345,
		GithubUsername: "testuser",
	}

	user, err := userRepo.CreateUser(user)
	require.NoError(t, err)

	// Test create multiple tokens
	_, err = tokenService.CreateToken(user.ID, "Token 1", 24*time.Hour)
	require.NoError(t, err)
	_, err = tokenService.CreateToken(user.ID, "Token 2", 48*time.Hour)
	require.NoError(t, err)

	// Test list tokens
	tokens, err := tokenService.ListUserTokens(user.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 2)

	// Test don't expose plain token, when listing
	for _, to := range tokens {
		require.Empty(t, to.PlainToken)
		require.NotEmpty(t, to.Hash)
		require.NotEmpty(t, to.Description)
		require.False(t, to.IsExpired())
	}
}
