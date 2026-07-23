-- Purges simulated (@void.com) accounts older than two hours, including all
-- their data. The user_id foreign keys have no ON DELETE CASCADE, so child
-- tables are cleared explicitly; transactions also cascade from uploads but
-- are deleted directly for clarity.
\set ON_ERROR_STOP on
BEGIN;

CREATE TEMP TABLE stale_users ON COMMIT DROP AS
  SELECT id, email FROM users
  WHERE email LIKE '%@void.com'
    AND created_at < now() - interval '2 hours';

SELECT count(*) AS accounts_to_purge FROM stale_users;

DELETE FROM transactions      WHERE user_id IN (SELECT id FROM stale_users);
DELETE FROM uploads           WHERE user_id IN (SELECT id FROM stale_users);
DELETE FROM category_rules    WHERE user_id IN (SELECT id FROM stale_users);
DELETE FROM custom_categories WHERE user_id IN (SELECT id FROM stale_users);
DELETE FROM users             WHERE id     IN (SELECT id FROM stale_users);

COMMIT;
