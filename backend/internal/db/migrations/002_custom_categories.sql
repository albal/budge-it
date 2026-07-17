-- User-defined spending categories, offered alongside the built-in list.
CREATE TABLE IF NOT EXISTS custom_categories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id),
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Case-insensitive uniqueness: "petrol" and "Petrol" are the same category.
CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_categories_user_name
    ON custom_categories (user_id, lower(name));
