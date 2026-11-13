# MessMate security hardening

## Production configuration

Set these environment variables on the hosting service:

- `APP_ENV=production`
- `APP_BASE_URL=https://messmate-zhms.onrender.com` (use the actual public HTTPS origin)
- `CORS_ALLOWED_ORIGINS` only for additional trusted browser origins; leave empty if the app is served from one origin.
- `JWT_SECRET` to a unique cryptographically random value of at least 32 characters.
- `DATABASE_URL` to the managed PostgreSQL connection string with TLS enabled where supported.

Do not commit real environment files, database URLs, JWT secrets, SMTP credentials, Razorpay credentials, or webhook secrets.

## Controls added in this branch

- Explicit CORS origin allowlist rather than wildcard CORS.
- Security response headers, API no-store cache policy, and a 1 MiB API request-body cap.
- Server read/write/header timeouts.
- In-process throttling for login, registration, password recovery, and mobile pairing endpoints.
- Six-digit mobile pairing codes generated using cryptographic randomness; pairing-code hashes are keyed with the JWT secret.
- Safer DOM construction for the Razorpay renewal dialog to avoid inserting consumer-controlled text as HTML.
- Exact direct frontend dependency versions, automated dependency update PRs, Go vulnerability scanning, npm audit, and build/test CI.

## Operational limitations and follow-up

The rate limiter is process-local. It reduces opportunistic guessing on a single running instance but is not a substitute for a shared Redis-backed limiter when running multiple instances or when traffic passes through a proxy that masks client IP addresses. In that setup, configure a trusted proxy/client-IP strategy and shared rate-limit storage.

The JWT is still stored in browser localStorage by the existing app. This branch reduces one DOM-XSS path but does not eliminate the impact of any future XSS. A separate migration to HttpOnly, Secure, SameSite cookies needs coordinated API, web, desktop, and mobile-client changes.

After merging, verify the configured production origin and run the deployed service through an external TLS/header scan and an authenticated authorization test matrix. Rotate any credentials that may ever have been committed to Git history; removing a secret from the latest commit does not invalidate earlier copies.
