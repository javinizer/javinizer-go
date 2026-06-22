package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

const hashTableName = "schema_migrations_hash"

func ensureMigrationHashTable(db *sql.DB) error {
	_, err := db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			migration_name TEXT PRIMARY KEY,
			content_hash TEXT NOT NULL,
			applied_at DATETIME DEFAULT (datetime('now'))
		)
	`, hashTableName))
	if err != nil {
		return fmt.Errorf("create migration hash table: %w", err)
	}
	return nil
}

func computeMigrationHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func StoreMigrationHash(db *sql.DB, name, hash string) error {
	_, err := db.Exec(fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (migration_name, content_hash)
		VALUES (?, ?)
	`, hashTableName), name, hash)
	if err != nil {
		return fmt.Errorf("store migration hash: %w", err)
	}
	return nil
}

func GetStoredHash(db *sql.DB, name string) (string, error) {
	var hash string
	err := db.QueryRow(fmt.Sprintf(`
		SELECT content_hash FROM %s WHERE migration_name = ?
	`, hashTableName), name).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get stored hash: %w", err)
	}
	return hash, nil
}
