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
    _, err = db.Exec(string(schema))
    return err
}
