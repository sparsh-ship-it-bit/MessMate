package reminders

import (
    "context"
    "database/sql"
    "log"
    "time"

    "github.com/sparsh-ship-it-bit/MessMate/internal/notifications"
)

// Worker checks subscriptions periodically. Replace NoopSender with a real
// approved WhatsApp Business provider in production.
type Worker struct { DB *sql.DB; Sender notifications.Sender; Interval time.Duration }

func (w Worker) Run(ctx context.Context) {
    if w.Interval <= 0 { w.Interval = time.Hour }
    ticker := time.NewTicker(w.Interval); defer ticker.Stop()
    w.process(ctx)
    for { select { case <-ctx.Done(): return; case <-ticker.C: w.process(ctx) } }
}

func (w Worker) process(ctx context.Context) {
    rows, err := w.DB.QueryContext(ctx, `SELECT c.phone,c.name,s.end_date,s.amount FROM consumers c JOIN subscriptions s ON s.consumer_id=c.id WHERE c.active AND s.payment_status <> 'paid' AND s.end_date BETWEEN CURRENT_DATE AND CURRENT_DATE + INTERVAL '1 day'`)
    if err != nil { log.Printf("reminder worker: %v", err); return }
    defer rows.Close()
    for rows.Next() {
        var phone,name string; var end time.Time; var amount float64
        if err := rows.Scan(&phone,&name,&end,&amount); err == nil {
            if err := w.Sender.SendExpiryReminder(ctx,phone,name,end.Format("2006-01-02"),amount); err != nil { log.Printf("reminder failed for %s: %v",phone,err) }
        }
    }
}
