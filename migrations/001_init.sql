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
    meal_plan VARCHAR(30) NOT NULL DEFAULT 'all',
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
    month_label VARCHAR(30) NOT NULL,
    payment_status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK(payment_status IN ('paid','pending','partial')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(end_date >= start_date)
);

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
