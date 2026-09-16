package notifications

import "context"

// Sender abstracts WhatsApp/SMS/push providers. The API can be wired to an
// approved provider without coupling subscription logic to a vendor.
type Sender interface {
    SendExpiryReminder(ctx context.Context, phone, consumerName string, expiryDate string, amount float64) error
}

type NoopSender struct{}
func (NoopSender) SendExpiryReminder(context.Context, string, string, string, float64) error { return nil }
