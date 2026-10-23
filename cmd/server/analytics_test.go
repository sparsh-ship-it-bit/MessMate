package main

import (
    "testing"
    "time"
)

func TestParseMonthUsesIndiaCalendar(t *testing.T) {
    start, end, err := parseMonth("2026-10")
    if err != nil {
        t.Fatalf("parseMonth returned error: %v", err)
    }
    if got := start.Format("2006-01-02 15:04 -0700"); got != "2026-10-01 00:00 +0530" {
        t.Fatalf("unexpected month start: %s", got)
    }
    if got := end.Format("2006-01-02"); got != "2026-10-31" {
        t.Fatalf("unexpected month end: %s", got)
    }
}

func TestDisplayedPaymentStatusUsesIndiaDate(t *testing.T) {
    endDate := time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)

    tests := []struct {
        name string
        paid float64
        amount float64
        now time.Time
        want string
    }{
        {"paid before expiry day ends in India", 100, 100, time.Date(2026, time.October, 22, 18, 0, 0, 0, time.UTC), "paid"},
        {"overdue exactly at midnight in India", 100, 100, time.Date(2026, time.October, 22, 18, 30, 0, 0, time.UTC), "overdue"},
		{"overdue after expiry day ends in India", 100, 100, time.Date(2026, time.October, 22, 19, 0, 0, 0, time.UTC), "overdue"},
        {"partial payment before expiry", 40, 100, time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC), "partial"},
        {"pending before expiry", 0, 100, time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC), "pending"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := displayedPaymentStatus(tt.paid, tt.amount, endDate, tt.now); got != tt.want {
                t.Fatalf("displayedPaymentStatus() = %q, want %q", got, tt.want)
            }
        })
    }
}

func TestPaymentStatusFromAmounts(t *testing.T) {
    tests := []struct {
        total, paid float64
        want string
    }{
        {100, 0, "pending"},
        {100, 35, "partial"},
        {100, 100, "paid"},
		{0, 0, "pending"},
    }
    for _, tt := range tests {
        if got := paymentStatus(tt.total, tt.paid); got != tt.want {
            t.Errorf("paymentStatus(%v, %v) = %q, want %q", tt.total, tt.paid, got, tt.want)
        }
    }
}
