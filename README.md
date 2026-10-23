# MessMate

MessMate is a backend-first B2B SaaS platform for mess owners to manage consumers, subscriptions, payments and QR-based meal attendance.

## MVP
- Owner registration/login with JWT
- Consumer profiles and unique consumer IDs
- Subscription and payment tracking
- Dashboard: active consumers, expiring today, pending payments, today's collections and attendance
- Unique QR codes per consumer
- Breakfast/lunch/dinner QR attendance with duplicate protection
- Subscription expiry detection
- Notification abstraction ready for WhatsApp/other providers

## Stack
Go, PostgreSQL, JWT, REST API

## Run
1. Copy `.env.example` to `.env`.
2. Start PostgreSQL.
3. Run `psql $DATABASE_URL -f migrations/001_init.sql`.
4. Run `go mod tidy`.
5. Run `go run ./cmd/server`.

Default server: `http://localhost:8080`

## Main endpoints
`POST /api/v1/auth/register`  
`POST /api/v1/auth/login`  
`GET /api/v1/dashboard`  
`POST /api/v1/consumers`  
`GET /api/v1/consumers`  
`GET /api/v1/consumers/:id`  
`PATCH /api/v1/consumers/:id`  
`DELETE /api/v1/consumers/:id`  
`POST /api/v1/subscriptions`  
`GET /api/v1/payments/pending`  
`POST /api/v1/attendance/scan`  
`GET /api/v1/attendance/today`

WhatsApp sending is intentionally represented by a provider interface; production WhatsApp automation should use an approved WhatsApp Business/Meta integration.

## Versioning
MessMate follows Semantic Versioning (MAJOR.MINOR.PATCH):
- PATCH (2.2.1): bug fixes, small corrections, and safe fixes that do not change the product's intended feature set.
- MINOR (2.3.0): new backward-compatible features or meaningful feature updates.
- MAJOR (3.0.0): major product changes, breaking changes, or a substantial new generation of the app.

The current app version is **2.2.1**.


## Payment behavior and verification

- Manual payment recording updates the payment ledger and subscription balance in one database transaction.
- Subscription payment status is derived from the amount paid and the subscription end date using India Standard Time (IST).
- The direct UPI QR flow creates a standard `upi://pay` QR with the selected consumer, UPI ID, and prefilled amount. It does **not** receive a payment confirmation from a bank by itself. The owner must verify the credit in their bank/UPI app and then record the payment in MessMate.
- Do not describe direct UPI QR collection as automatic payment verification. Automatic confirmation requires a supported payment provider integration and a configured, verified webhook.

## Local checks

Run these before deploying:

```sh
go test ./...
go vet ./...
```

For the frontend:

```sh
cd web
npm ci
npm run build
```

## Pre-sale production checklist

- [ ] Rotate any database credentials that may have been shared or exposed; keep secrets only in the deployment environment.
- [ ] Verify that `JWT_SECRET` is unique and strong in production.
- [ ] Test with two separate owner accounts to verify tenant isolation.
- [ ] Test payment creation, partial payments, full payments, expired subscriptions, and date boundaries in IST.
- [ ] Test the UPI QR with a small real payment and manually verify the bank credit before recording it.
- [ ] Verify backup and restore procedures before using real customer data.
- [ ] Review the deployed build and Render logs after each deployment.
