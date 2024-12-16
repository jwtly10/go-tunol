package testutil

import (
	"github.com/jwtly10/go-tunol/internal/config"
	"github.com/jwtly10/go-tunol/internal/db"
	"os"
	"testing"
)

// SetupTestDB creates a new SQLite database for testing and applies all migrations.
func SetupTestDB(t *testing.T) (*db.Database, func()) {
	t.Helper()

	tmpfile, err := os.CreateTemp("", "test-*.db")
	if err != nil {
		t.Fatalf("Could not create temporary file: %v", err)
	}
	tmpfile.Close()

	database, err := db.Initialize(config.DatabaseConfig{
		Path: tmpfile.Name(),
	})
	if err != nil {
		os.Remove(tmpfile.Name())
		t.Fatalf("Could not initialize database: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.Remove(tmpfile.Name())
	}

	return database, cleanup
}
