package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// applyMigrations reads all migration files from the migrations directory
func applyMigrations(db *sql.DB) error {
	fmt.Println("Applying migrations")

	// Find migrations dir
	projectRoot, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to find project root: %w", err)
	}
	migrationsDir := filepath.Join(projectRoot, "db", "migrations")

	fmt.Printf("Using migrations directory: %s\n", migrationsDir)

	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Create migrations table if it doesn't exist
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
            filename TEXT PRIMARY KEY,
			query TEXT,
            applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );
    `)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Apply each migration in order
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".sql" {
			continue
		}

		// Check if migration has already been applied
		var exists bool
		err = db.QueryRow("SELECT 1 FROM schema_migrations WHERE filename = ?", file.Name()).Scan(&exists)
		if err != sql.ErrNoRows && err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}
		if exists {
			continue // Skip already applied migrations
		}

		// Read and execute migration
		migrationPath := filepath.Join(migrationsDir, file.Name())
		migrationSQL, err := os.ReadFile(migrationPath)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file.Name(), err)
		}

		// Begin transaction for migration
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %s: %w", file.Name(), err)
		}

		// Execute migration
		if _, err = tx.Exec(string(migrationSQL)); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute migration %s: %w", file.Name(), err)
		}

		// Record migration
		if _, err = tx.Exec("INSERT INTO schema_migrations (filename, query) VALUES (?, ?)", file.Name(), migrationSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", file.Name(), err)
		}

		// Commit transaction
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", file.Name(), err)
		}

		fmt.Printf("Applied migration: %s\n", file.Name())
	}

	return nil
}

// findProjectRoot finds the root directory of the project by looking for a go.mod file
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent directory")
		}
		dir = parent
	}
}
