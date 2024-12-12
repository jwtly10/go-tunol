package auth

import (
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID              int64
	GithubID        int64
	GithubUsername  string
	GithubAvatarURL string
	GithubEmail     string
	CreatedAt       time.Time
	LastLogin       *time.Time // May be nil if never logged in
}

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) CreateUser(user *User) (*User, error) {
	result, err := r.db.Exec(`
        INSERT INTO users (github_id, github_username, github_avatar_url, github_email, created_at)
        VALUES (?, ?, ?, ?, ?)
    `, user.GithubID, user.GithubUsername, user.GithubAvatarURL, user.GithubEmail, time.Now())

	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Just query this insert worked so we are certain we have all the latest data when returning and checking sessions
	var created User
	err = r.db.QueryRow(`
        SELECT id, github_id, github_username, github_avatar_url, github_email, created_at, last_login
        FROM users 
        WHERE id = ?
    `, id).Scan(
		&created.ID,
		&created.GithubID,
		&created.GithubUsername,
		&created.GithubAvatarURL,
		&created.GithubEmail,
		&created.CreatedAt,
		&created.LastLogin,
	)

	if err != nil {
		return nil, err
	}

	return &created, nil
}

func (r *UserRepository) CreateOrUpdateUser(user *User) (*User, error) {
	existing, err := r.FindByGithubID(user.GithubID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		return r.CreateUser(user)
	}

	_, err = r.db.Exec(`
		UPDATE users
		SET github_username = ?, github_avatar_url = ?, github_email = ?, last_login = ?
		WHERE github_id = ?
	`, user.GithubUsername, user.GithubAvatarURL, user.GithubEmail, time.Now(), user.GithubID)

	if err != nil {
		return nil, err
	}

	// Just query this insert worked so we are certain we have all the latest data when returning and checking sessions
	var updated User
	err = r.db.QueryRow(`
        SELECT id, github_id, github_username, github_avatar_url, github_email, created_at, last_login
        FROM users 
        WHERE github_id = ?
    `, user.GithubID).Scan(
		&updated.ID,
		&updated.GithubID,
		&updated.GithubUsername,
		&updated.GithubAvatarURL,
		&updated.GithubEmail,
		&updated.CreatedAt,
		&updated.LastLogin,
	)

	if err != nil {
		return nil, err
	}

	return &updated, nil
}

func (r *UserRepository) FindByGithubID(githubID int64) (*User, error) {
	user := &User{}
	err := r.db.QueryRow(`
        SELECT id, github_id, github_username, github_avatar_url, github_email, created_at, last_login
        FROM users
        WHERE github_id = ?
    `, githubID).Scan(
		&user.ID,
		&user.GithubID,
		&user.GithubUsername,
		&user.GithubAvatarURL,
		&user.GithubEmail,
		&user.CreatedAt,
		&user.LastLogin,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func (r *UserRepository) FindByID(userId int64) (*User, error) {
	user := &User{}
	err := r.db.QueryRow(`
		SELECT id, github_id, github_username, github_avatar_url, github_email, created_at, last_login
		FROM users
		WHERE id = ?
	`, userId).Scan(
		&user.ID,
		&user.GithubID,
		&user.GithubUsername,
		&user.GithubAvatarURL,
		&user.GithubEmail,
		&user.CreatedAt,
		&user.LastLogin,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}
