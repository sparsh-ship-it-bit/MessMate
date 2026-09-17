package main

import (
    "database/sql"
    "errors"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
    _ "github.com/lib/pq"
)

// registerWeb serves the compiled React application from the same origin as the API.
// It also registers the one-click renewal endpoint used by the Subscriptions screen.
func registerWeb(mux *http.ServeMux) {
    mux.HandleFunc("POST /api/v1/subscriptions/renew", renewSubscription)
    dir := os.Getenv("WEB_DIR")
    if dir == "" {
        dir = "web/dist"
    }
    mux.Handle("/", http.FileServer(http.Dir(dir)))
}

type renewalRequest struct {
    ConsumerID string `json:"consumer_id"`
    Period     string `json:"period"`
}

func renewSubscription(w http.ResponseWriter, r *http.Request) {
    token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
    if token == "" {
        errorJSON(w, http.StatusUnauthorized, "missing bearer token")
        return
    }
    claims := &OwnerClaims{}
    parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
        if t.Method != jwt.SigningMethodHS256 { return nil, errors.New("invalid signing method") }
        return []byte(getenv("JWT_SECRET", "dev-secret-change-me")), nil
    })
    if err != nil || !parsed.Valid {
        errorJSON(w, http.StatusUnauthorized, "invalid or expired token")
        return
    }
    ownerID, err := uuid.Parse(claims.OwnerID)
    if err != nil { errorJSON(w, http.StatusUnauthorized, "invalid owner id"); return }

    var req renewalRequest
    if decode(r, &req) != nil || req.ConsumerID == "" {
        errorJSON(w, http.StatusBadRequest, "consumer_id is required")
        return
    }
    if req.Period != "half" && req.Period != "full" {
        errorJSON(w, http.StatusBadRequest, "period must be half or full")
        return
    }
    consumerID, err := uuid.Parse(req.ConsumerID)
    if err != nil { errorJSON(w, http.StatusBadRequest, "invalid consumer_id"); return }

    db, err := sql.Open("postgres", getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/messmate?sslmode=disable"))
    if err != nil { errorJSON(w, 500, "database connection failed"); return }
    defer db.Close()
    if err = db.Ping(); err != nil { errorJSON(w, 500, "database connection failed"); return }

    var previousEnd time.Time
    var monthlyAmount float64
    err = db.QueryRow(`
        SELECT s.end_date, COALESCE(NULLIF(s.monthly_amount,0),s.amount)
        FROM subscriptions s
        JOIN consumers c ON c.id=s.consumer_id
        WHERE s.consumer_id=$1 AND c.owner_id=$2
        ORDER BY s.end_date DESC LIMIT 1`, consumerID, ownerID).Scan(&previousEnd, &monthlyAmount)
    if err != nil { errorJSON(w, 404, "consumer subscription not found"); return }
    if monthlyAmount <= 0 { errorJSON(w, 400, "consumer monthly amount is not configured"); return }

    today := time.Now().Truncate(24 * time.Hour)
    start := today
    if !previousEnd.IsZero() && !previousEnd.Before(today) {
        start = previousEnd.AddDate(0, 0, 1)
    }

    amount := monthlyAmount
    end := start.AddDate(0, 1, -1)
    if req.Period == "half" {
        amount = monthlyAmount / 2
        end = start.AddDate(0, 0, 14)
    }

    tx, err := db.Begin()
    if err != nil { errorJSON(w, 500, "could not start renewal"); return }
    defer tx.Rollback()

    var newID uuid.UUID
    status := paymentStatus(amount, amount)
    err = tx.QueryRow(`
        INSERT INTO subscriptions(consumer_id,start_date,end_date,amount,monthly_amount,amount_paid,month_label,payment_status)
        VALUES($1,$2,$3,$4,$5,$4,$6,$7) RETURNING id`,
        consumerID, start, end, amount, monthlyAmount, start.Format("January 2006"), status).Scan(&newID)
    if err != nil { errorJSON(w, 500, "could not create renewal"); return }

    if _, err = tx.Exec(`INSERT INTO payments(consumer_id,subscription_id,amount,method) VALUES($1,$2,$3,'cash')`, consumerID, newID, amount); err != nil {
        errorJSON(w, 500, "could not record renewal payment")
        return
    }
    if err = tx.Commit(); err != nil { errorJSON(w, 500, "could not save renewal"); return }

    writeJSON(w, http.StatusCreated, map[string]any{
        "subscription_id": newID,
        "consumer_id": consumerID,
        "period": req.Period,
        "start_date": start.Format("2006-01-02"),
        "end_date": end.Format("2006-01-02"),
        "monthly_amount": monthlyAmount,
        "amount_paid": amount,
        "payment_status": status,
    })
}
