package auth

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUserRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)

	user := &User{
		GithubID:       12345,
		GithubUsername: "testuser",
		GithubEmail:    "test@example.com",
	}

	// Test user create
	user, err := repo.CreateUser(user)
	require.NoError(t, err)
	// The id is auto-incremented, so we should set it to 1, since it's the first user in this test
	require.Equal(t, int64(1), user.ID)

	// Test finding user by GitHub ID
	found, err := repo.FindByGithubID(user.GithubID)
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, user.GithubUsername, found.GithubUsername)

	// Test non-existent user
	notFound, err := repo.FindByGithubID(99999)
	require.NoError(t, err)
	require.Nil(t, notFound)
}
