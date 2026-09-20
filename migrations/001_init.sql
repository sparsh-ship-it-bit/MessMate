CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS owners (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(120) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    phone VARCHAR(20),
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS consumers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_id UUID NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
    consumer_id VARCHAR(50) NOT NULL,
    name VARCHAR(120) NOT NULL,
    phone VARCHAR(20) NOT NULL,
    meal_plan VARCHAR(100) NOT NULL DEFAULT 'all',
    qr_token UUID NOT NULL UNIQUE DEFAULT uuid_generate_v4(),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(owner_id, consumer_id)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    consumer_id UUID NOT NULL REFERENCES consumers(id) ON DELETE CASCADE,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    amount NUMERIC(12,2) NOT NULL CHECK(amount >= 0),
    monthly_amount NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK(monthly_amount >= 0),
    amount_paid NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK(amount_paid >= 0),
    month_label VARCHAR(30) NOT NULL,
    payment_status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK(payment_status IN ('paid','pending','partial')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(end_date >= start_date),
    CHECK(amount_paid <= amount)
);

ALTER TABLE owners ADD COLUMN IF NOT EXISTS upi_id VARCHAR(120);
ALTER TABLE consumers ALTER COLUMN meal_plan TYPE VARCHAR(100);
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS amount_paid NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS monthly_amount NUMERIC(12,2);
UPDATE subscriptions SET monthly_amount = amount WHERE monthly_amount IS NULL OR monthly_amount = 0;
ALTER TABLE subscriptions ALTER COLUMN monthly_amount SET DEFAULT 0;
ALTER TABLE subscriptions ALTER COLUMN monthly_amount SET NOT NULL;

CREATE OR REPLACE FUNCTION messmate_set_monthly_amount()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.monthly_amount IS NULL OR NEW.monthly_amount = 0 THEN
        NEW.monthly_amount := NEW.amount;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_subscription_monthly_amount ON subscriptions;
CREATE TRIGGER trg_subscription_monthly_amount
BEFORE INSERT OR UPDATE OF amount, monthly_amount ON subscriptions
FOR EACH ROW EXECUTE FUNCTION messmate_set_monthly_amount();

CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    consumer_id UUID NOT NULL REFERENCES consumers(id) ON DELETE CASCADE,
    subscription_id UUID REFERENCES subscriptions(id) ON DELETE SET NULL,
    amount NUMERIC(12,2) NOT NULL CHECK(amount > 0),
    method VARCHAR(30) NOT NULL DEFAULT 'cash',
    reference VARCHAR(100),
    paid_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS attendance (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    consumer_id UUID NOT NULL REFERENCES consumers(id) ON DELETE CASCADE,
    attendance_date DATE NOT NULL,
    meal VARCHAR(20) NOT NULL CHECK(meal IN ('breakfast','lunch','dinner')),
    scanned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(consumer_id, attendance_date, meal)
);

CREATE INDEX IF NOT EXISTS idx_consumers_owner ON consumers(owner_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_end ON subscriptions(end_date);
CREATE INDEX IF NOT EXISTS idx_payments_consumer ON payments(consumer_id);
CREATE INDEX IF NOT EXISTS idx_attendance_date ON attendance(attendance_date);

ALTER TABLE owners ADD COLUMN IF NOT EXISTS razorpay_account_id VARCHAR(80);

CREATE TABLE IF NOT EXISTS payment_intents (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
    consumer_id UUID NOT NULL REFERENCES consumers(id) ON DELETE CASCADE,
    month_label VARCHAR(7) NOT NULL,
    period VARCHAR(10) NOT NULL CHECK(period IN ('half','full')),
    amount NUMERIC(12,2) NOT NULL CHECK(amount > 0),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','paid','failed')),
    provider_qr_id VARCHAR(100),
    provider_payment_id VARCHAR(100),
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_intents_provider_payment ON payment_intents(provider_payment_id) WHERE provider_payment_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_payment_intents_owner ON payment_intents(owner_id);
CREATE INDEX IF NOT EXISTS idx_payment_intents_qr ON payment_intents(provider_qr_id);
