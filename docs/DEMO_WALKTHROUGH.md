# MessMate Buyer Demo Walkthrough

Target length: 2–4 minutes. Record only after the paths below pass the acceptance tests in [ACQUISITION_PACK.md](ACQUISITION_PACK.md).

## Before recording

- Use a clean demo owner account and fictional test data.
- Close personal apps and hide notifications.
- Never show passwords, API keys, JWTs, database URLs, private customer details, bank account details, or production environment variables.
- Confirm the exact build/version and date.
- If a workflow fails, stop recording and fix it or disclose the limitation; do not edit the video to imply a failed workflow passed.

## Suggested narration and shots

### 0:00–0:15 — Introduction
“MessMate is a mess-management software project intended to help operators manage consumers, subscriptions, payment records, and QR-based meal attendance. This is a demonstration of the build and features shown today; live production reliability and future development are not guaranteed.”

### 0:15–0:40 — Owner dashboard
Show the dashboard and explain each visible metric. Only state that figures are accurate if they have been reconciled against the demo database.

### 0:40–1:10 — Consumer management
Create a fictional consumer, show the generated consumer identifier, and demonstrate editing or viewing the record.

### 1:10–1:40 — Subscription and payment ledger
Create a test subscription and record a test payment. Show the paid amount and remaining balance. Explain that payment status is derived from recorded payment data and dates, where that behavior has been verified.

### 1:40–2:10 — QR attendance
Show the consumer QR code and a successful attendance scan, including duplicate-scan behavior if verified.

### 2:10–2:30 — Payment collection disclosure
“If using the direct UPI QR flow, the QR pre-fills payment details; it does not by itself confirm a bank credit. The operator must verify receipt and record the payment unless a separately configured and tested provider integration is in place.”

### 2:30–3:00 — Technical overview and close
Briefly state the verified stack and explain that source code, setup documentation, and an agreed handover are included only as specified in the final sale agreement. Mention any known limitations openly.

## Recording quality checklist

- [ ] All demonstrated flows passed immediately before recording.
- [ ] No real personal, financial, or customer data appears.
- [ ] No secrets or credentials appear.
- [ ] Narration matches actual product behavior.
- [ ] The version, commit, and recording date are recorded in the filename/notes.
- [ ] Known limitations are disclosed to the buyer.
