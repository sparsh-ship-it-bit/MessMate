package main

import (
    "context"
    "database/sql"
    "log"
    "os"
    "time"

    _ "github.com/lib/pq"
    "github.com/sparsh-ship-it-bit/MessMate/internal/notifications"
    "github.com/sparsh-ship-it-bit/MessMate/internal/reminders"
)

func main() {
    dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable")
    db, err := sql.Open("postgres", dsn); if err != nil { log.Fatal(err) }
    if err := db.Ping(); err != nil { log.Fatal(err) }
    log.Println("MessMate reminder worker started")
    reminders.Worker{DB: db, Sender: notifications.NoopSender{}, Interval: time.Hour}.Run(context.Background())
}

func getenv(k, fallback string) string { if v := os.Getenv(k); v != "" { return v }; return fallback }
