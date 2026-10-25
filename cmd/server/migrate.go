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
    if err != nil { return err }
    _, err = db.Exec(`CREATE TABLE IF NOT EXISTS mobile_call_requests (id UUID PRIMARY KEY DEFAULT uuid_generate_v4(), owner_id UUID NOT NULL REFERENCES owners(id) ON DELETE CASCADE, pairing_id UUID NOT NULL REFERENCES mobile_pairings(id) ON DELETE CASCADE, consumer_id UUID NOT NULL REFERENCES consumers(id) ON DELETE CASCADE, consumer_name VARCHAR(120) NOT NULL, phone VARCHAR(40) NOT NULL, status VARCHAR(20) NOT NULL DEFAULT 'pending', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), expires_at TIMESTAMPTZ NOT NULL, delivered_at TIMESTAMPTZ)`)
    if err != nil { return err }
    _, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_mobile_call_requests_pairing ON mobile_call_requests(pairing_id, status, created_at)`)
    return err
}
