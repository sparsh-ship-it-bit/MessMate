# MessMate password-recovery email setup

Password reset is implemented in the Go API. Reset links expire after 30 minutes and can be used once. The API stores only a SHA-256 hash of each reset token.

## Configure SMTP on Render

1. Open the **messmate** web service in the Render Dashboard and open **Environment**.
2. Confirm these settings exist:
   - `APP_BASE_URL=https://messmate-zhms.onrender.com`
   - `SMTP_HOST=smtp.gmail.com`
   - `SMTP_PORT=587`
3. Set the following values in Render's Environment page:
   - `SMTP_USERNAME`: the full Gmail address that will send the email.
   - `SMTP_PASSWORD`: a Google **App Password** for that account, not the normal Gmail password.
   - `SMTP_FROM`: the same sender email address.
   - `FEEDBACK_EMAIL`: the inbox that should receive feedback submissions (can be a different address).
4. In the sending Google account, enable 2-Step Verification, then create an App Password from Google Account → Security → App passwords. If App Passwords are unavailable, the account's policy may not allow them; use an SMTP provider and its SMTP credentials instead.
5. Save the environment changes in Render and wait for the service to finish redeploying.

Keep the App Password only in Render's secret environment setting. Do not commit it to Git, place it in `.env.example`, or send it in chat or issue comments.

## Verify the complete flow

1. Open the deployed app and select **Forgot password?**.
2. Enter the email of an existing MessMate owner account.
3. Confirm that the reset email arrives (check Spam/Junk too) and open the link within 30 minutes.
4. Set a new password of at least 8 characters.
5. Sign in using the same account and the new password.
6. Request another reset and confirm an already-used link cannot reset the password again.

The forgot-password API intentionally returns a generic message so it does not disclose whether an email is registered. A missing SMTP configuration returns HTTP 503; an SMTP send failure returns HTTP 502. Do not treat a generic UI message alone as proof that an email arrived.

## What still requires account access

Repository changes can declare the required environment variables, but they cannot create a Google App Password or set private values in the Render Dashboard. Those steps must be completed by someone with access to the sending Google account and Render service.
