package database

import (
	"database/sql"
	"fmt"
	"github.com/jwtly10/go-tunol/pkg/config"
	_ "github.com/mattn/go-sqlite3"
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
