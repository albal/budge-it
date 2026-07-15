CREATE TABLE IF NOT EXISTS users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Single-tenant default user until authentication lands.
INSERT INTO users (id, email)
VALUES ('00000000-0000-0000-0000-000000000001', 'default@budge-it.local')
ON CONFLICT (email) DO NOTHING;

CREATE TABLE IF NOT EXISTS uploads (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users (id),
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes   BIGINT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    error        TEXT NOT NULL DEFAULT '',
    object_key   TEXT NOT NULL DEFAULT '',
    txn_count    INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_uploads_user_status ON uploads (user_id, status);

CREATE TABLE IF NOT EXISTS transactions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id),
    upload_id   UUID NOT NULL REFERENCES uploads (id) ON DELETE CASCADE,
    txn_date    DATE NOT NULL,
    description TEXT NOT NULL,
    merchant    TEXT NOT NULL,
    amount      NUMERIC(14, 2) NOT NULL CHECK (amount >= 0),
    direction   TEXT NOT NULL CHECK (direction IN ('debit', 'credit')),
    category    TEXT NOT NULL DEFAULT 'Uncategorized',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_txn_user_date ON transactions (user_id, txn_date);
CREATE INDEX IF NOT EXISTS idx_txn_user_category ON transactions (user_id, category);

CREATE TABLE IF NOT EXISTS category_rules (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id),
    pattern    TEXT NOT NULL,
    category   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, pattern)
);
