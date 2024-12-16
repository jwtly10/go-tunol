package database

import (
	"database/sql"
	"fmt"
	"github.com/jwtly10/go-tunol/pkg/config"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
	"sync"
)

var (
	db   *sql.DB
	once sync.Once
)

type Config struct {
	Path string
}

func Initialize(cfg config.DatabaseConfig) error {
	var initErr error
	once.Do(func() {
		var err error

		dbDir := filepath.Dir(cfg.Path)
		if err = os.MkdirAll(dbDir, 0755); err != nil {
			initErr = fmt.Errorf("failed to create database directory: %w", err)
			return
		}

		fmt.Printf("Attempting to connect to database at: %s\n", cfg.Path)
		db, err = sql.Open("sqlite3", cfg.Path)
		if err != nil {
			initErr = err
			return
		}

		// Validate the connection
		if err = db.Ping(); err != nil {
			initErr = err
			return
		}

		if err = applyMigrations(db); err != nil {
			initErr = fmt.Errorf("failed to apply migrations: %w", err)
			return
		}

		// List all tables in the database
		// used for debugging just in case
		rows, err := db.Query(`
            SELECT name FROM sqlite_master 
            WHERE type='table' 
            ORDER BY name;
        `)
		if err != nil {
			fmt.Printf("Error querying tables: %v\n", err)
		} else {
			fmt.Println("Tables in database:")
			for rows.Next() {
				var name string
				rows.Scan(&name)
				fmt.Printf("- %s\n", name)
			}
			rows.Close()
		}
	})
	return initErr
}

// GetDB returns the database instance.
// Panics if the database hasn't been initialized.
func GetDB() *sql.DB {
	if db == nil {
		panic("Database not initialized. Call Initialize() first")
	}
	return db
}

// Close closes the database connection.
func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

// applyMigrations reads all migration files from the migrations directory
func applyMigrations(db *sql.DB) error {
	fmt.Println("Applying migrations")
	// Path to migrations directory relative to the project root
	migrationsDir := "./pkg/db/migrations"

	// Read all migration files
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Create migrations table if it doesn't exist
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
            filename TEXT PRIMARY KEY,
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
		if _, err = tx.Exec("INSERT INTO schema_migrations (filename) VALUES (?)", file.Name()); err != nil {
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
