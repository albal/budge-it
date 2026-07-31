-- Administrator flag. The first account to log in claims it; see
-- store.GetOrCreateUserByEmail for the claim, which is serialized so
-- concurrent first logins cannot both win.
--
-- Existing databases already have users, so nobody holds the flag after this
-- migration and the next login claims it. Use ADMIN_EMAILS to override, or
-- promote a specific account:
--   UPDATE users SET is_admin = true WHERE email = 'you@example.com';
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT false;

-- Both queries against this column ask "does any administrator exist" or "is
-- this user one", so only the true rows are worth indexing.
CREATE INDEX IF NOT EXISTS idx_users_is_admin ON users (is_admin) WHERE is_admin;
