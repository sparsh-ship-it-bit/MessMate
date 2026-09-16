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
