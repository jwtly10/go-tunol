package utils

import (
	"database/sql"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func SetupTestDB(t *testing.T) *sql.DB {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	// Read and execute all migrations
	migrationsDir := "../../pkg/db/migrations"
	files, err := os.ReadDir(migrationsDir)
	require.NoError(t, err)

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".sql" {
			continue
		}

		migrationPath := filepath.Join(migrationsDir, file.Name())
		migrationSQL, err := os.ReadFile(migrationPath)
		require.NoError(t, err)

		_, err = db.Exec(string(migrationSQL))
		require.NoError(t, err, "failed to execute migration: %s", file.Name())
	}

	return db
}
