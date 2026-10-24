package main

import (
    "database/sql"
    "os"
)

func runMigrations(db *sql.DB) error {
    path := os.Getenv("MIGRATION_FILE")
    if path == "" {
        path = "migrations/001_init.sql"
    }
    schema, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    if _, err = db.Exec(string(schema)); err != nil { return err }
    // Safe additive migration for existing databases; keeps current consumer data intact.
    _, err = db.Exec(`ALTER TABLE mobile_pairings ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ`)
    return err
}
