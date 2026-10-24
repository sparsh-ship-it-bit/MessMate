# MessMate Acquisition Readiness Pack

Last updated: 2026-10-24

## Sale status

- Intended transaction: permanent sale of the MessMate software/business assets.
- Paying customers: none reported.
- Revenue and profit: pre-revenue; do not claim recurring revenue, profitability, or customer traction.
- Initial asking price discussed by the seller: INR 200,000, negotiable. This is a proposed asking price, not a formal valuation or an offer.
- Repository: https://github.com/sparsh-ship-it-bit/MessMate
- Stack indicated by repository: Go REST API, PostgreSQL, JWT authentication, React/Vite web frontend, Docker/Render deployment configuration.
- Current live production health: NOT VERIFIED by this document.
- Current Android build health: NOT VERIFIED by this document.

## Product scope (verify each item in the current deployed build)

MessMate is intended for mess/hostel food-service operators. Repository documentation describes:

- Owner registration and login using JWT.
- Consumer profiles and unique consumer IDs.
- Subscription creation and expiry tracking.
- Payment ledger and payment-status tracking.
- Dashboard metrics for active consumers, expiring subscriptions, pending payments, collections, and attendance.
- Consumer-specific QR codes.
- Breakfast/lunch/dinner attendance with duplicate protection.
- A notification abstraction intended for future provider integrations.

Do not advertise any feature as production-ready until it passes the acceptance checks below.

## Payment disclosure

The repository README describes manual payment recording and a direct UPI QR flow. A UPI QR with a prefilled amount does not itself confirm that money reached the owner's bank account. Unless a supported payment provider and verified webhook are configured and tested, describe UPI QR collection as a payment instruction flow that requires the owner to verify receipt and record the payment.

Do not claim automatic payment confirmation without evidence.

## Known / previously reported risks to re-test

These are reported historical issues, not a statement that every issue is still present:

- Android app reportedly opened briefly and then closed.
- Database errors were reported in consumer/payment/analytics workflows.
- Consumer listing/creation and analytics accuracy were previously under investigation.
- Subscription/payment status timing and calculations were reported as incorrect.
- Recent payment-flow changes were made, but successful end-to-end live verification was not confirmed in the available project notes.
- Repository README reports app version 2.3.0 while web/package.json reports 2.4.0. Reconcile versioning before presenting a release to buyers.
- Render configuration specifies a free service/database plan. Confirm current provider limits, uptime expectations, backup options, and upgrade costs before sale.

Treat all items as OPEN until tested and recorded with date, environment, and evidence.

## Minimum acceptance test plan

Record pass/fail, date, build/commit, and evidence for each test.

1. Install/open the Android app on at least two Android devices; sign in and navigate without a crash.
2. Register and sign in as owner; verify invalid credentials are rejected.
3. Create, edit, list, and deactivate a consumer.
4. Create a subscription; verify start/end dates, monthly amount, amount paid, outstanding balance, and status.
5. Record no payment, partial payment, full payment, and an overpayment attempt; confirm correct ledger and balance behavior.
6. Verify status across expiry boundaries in India Standard Time (IST).
7. Generate and scan consumer QR codes; test breakfast/lunch/dinner and duplicate-scan protection.
8. Verify dashboard totals against the underlying records using a controlled test dataset.
9. Use two separate owner accounts and confirm neither can read or change the other's data.
10. Test the UPI QR flow using a small controlled transaction; verify bank receipt manually and do not assume the QR confirms payment.
11. Confirm database errors are handled safely and inspect Render logs after testing.
12. Test database backup and restore into a non-production environment.
13. Run backend checks: `go test ./...` and `go vet ./...`.
14. Run frontend build: `cd web && npm ci && npm run build`.
15. Confirm the production build, API health endpoint, domain, and deployment configuration work after a fresh deploy.

Do not use real customer information for acceptance testing unless necessary and properly authorized.

## Buyer-facing demo recording checklist

- Use a clean demo tenant and fictional test data only.
- Hide personal notifications, email addresses, bank details, access tokens, database URLs, API keys, and environment variables.
- Show owner login, consumer creation, subscription, payment ledger, dashboard, and QR attendance only after each path has passed.
- Explain manual UPI verification accurately.
- Record the version/commit and date of the demo.
- Keep the recording short (about 2–4 minutes) and include a brief note on known limitations.
- Do not describe the product as bug-free or production-proven unless test evidence supports that claim.

See [DEMO_WALKTHROUGH.md](DEMO_WALKTHROUGH.md) for the suggested narration.

## Technical handover inventory

Before transfer, prepare and verify:

- Source repository and exact release commit.
- Build/run instructions and dependency versions.
- Database schema/migrations and a sanitized sample dataset.
- Deployment service, domain/DNS, database, monitoring, and backup inventory.
- List of environment-variable NAMES only; never commit or send secret VALUES in this file.
- Third-party services and accounts, billing status, renewal dates, and transferability.
- Android package/build process and signing-key ownership/transfer plan, if applicable.
- Brand assets, domain ownership, app-store listing status (if any), and documentation.
- Open bug list and acceptance-test evidence.
- List of libraries and their licenses; verify commercial redistribution rights.
- Confirmation that no third-party, AI-generated, contractor-created, or open-source component is being transferred beyond the rights the seller actually holds.

Do not put production credentials, signing keys, private customer data, or live database dumps in the repository.

## Ownership and legal checklist

Before making a binding representation, confirm:

- The seller has the right to transfer the code, brand, domain, design assets, and any included accounts.
- Any collaborators/contractors have assigned the relevant rights in writing, where required.
- Third-party library and service terms allow the intended transfer and continued use.
- Personal data is not casually transferred. Plan a lawful, documented data handover or deletion process.
- The agreement clearly lists included/excluded assets, consideration, payment milestones, transfer sequence, warranties, known defects, liabilities, confidentiality, and post-sale support.
- The agreement specifies whether the seller provides any transition support and for how long.
- An India-qualified lawyer/accountant reviews the final agreement and applicable tax/GST treatment.

This checklist is operational guidance, not legal or tax advice.

## Suggested transfer sequence

1. Qualify the buyer and agree on the asset inventory and price range.
2. Share the demo and high-level technical pack; provide repository access only as appropriate.
3. Allow technical due diligence using a controlled, sanitized environment.
4. Sign a definitive asset purchase agreement and any confidentiality agreement.
5. Receive cleared funds or use a mutually accepted escrow process.
6. Transfer assets in the agreed sequence: repository ownership/clean export, domain, deployment, app listings, and other transferable accounts.
7. Rotate/revoke seller credentials, rotate all secrets, and verify buyer access.
8. Delete or retain seller copies only as the agreement and legal obligations require.
9. Complete a signed handover checklist and any agreed support period.

## Status tracking

- [ ] Product acceptance tests completed with evidence.
- [ ] Android crash resolved or clearly disclosed.
- [ ] Database, payment, and analytics issues resolved or clearly disclosed.
- [ ] App version numbers reconciled.
- [ ] README and clean setup tested from a fresh clone.
- [ ] Secrets scan completed; any exposed credentials rotated.
- [ ] Ownership and third-party license review completed.
- [ ] Backup/restore procedure tested.
- [ ] Demo video recorded and reviewed.
- [ ] Asset purchase agreement reviewed by a qualified professional.
