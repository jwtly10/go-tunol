package db

import (
	"database/sql"
	"github.com/jwtly10/go-tunol/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	*sql.DB
}

func Initialize(cfg config.DatabaseConfig) (*Database, error) {
	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if err := applyMigrations(db); err != nil {
		return nil, err
	}

	return &Database{db}, nil
}
