// Package store is the data-access layer over PostgreSQL.
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/budge-it/backend/internal/categorize"
	"github.com/budge-it/backend/internal/models"
)

// DefaultUserID is the single-tenant user seeded by the initial migration.
const DefaultUserID = "00000000-0000-0000-0000-000000000001"

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// --- uploads ---

func (s *Store) CreateUpload(ctx context.Context, u *models.Upload) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO uploads (user_id, filename, content_type, size_bytes, status, object_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`,
		u.UserID, u.Filename, u.ContentType, u.SizeBytes, u.Status, u.ObjectKey,
	).Scan(&u.ID, &u.CreatedAt)
}

func (s *Store) GetUpload(ctx context.Context, userID, id string) (*models.Upload, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, filename, content_type, size_bytes, status, error, object_key, txn_count, created_at, processed_at
		FROM uploads WHERE user_id = $1 AND id = $2`, userID, id)
	return scanUpload(row)
}

func (s *Store) ListUploads(ctx context.Context, userID string) ([]*models.Upload, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, filename, content_type, size_bytes, status, error, object_key, txn_count, created_at, processed_at
		FROM uploads WHERE user_id = $1 ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	uploads := []*models.Upload{}
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, u)
	}
	return uploads, rows.Err()
}

// PendingUploadIDs returns uploads that never finished (e.g. the pod was
// restarted mid-job) so the worker queue can pick them up again.
func (s *Store) PendingUploadIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM uploads WHERE status IN ('pending','processing') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) GetUploadByID(ctx context.Context, id string) (*models.Upload, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, filename, content_type, size_bytes, status, error, object_key, txn_count, created_at, processed_at
		FROM uploads WHERE id = $1`, id)
	return scanUpload(row)
}

func (s *Store) MarkUploadProcessing(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE uploads SET status='processing' WHERE id=$1`, id)
	return err
}

func (s *Store) MarkUploadDone(ctx context.Context, id string, txnCount int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE uploads SET status='done', txn_count=$2, error='', object_key='', processed_at=now()
		WHERE id=$1`, id, txnCount)
	return err
}

func (s *Store) MarkUploadError(ctx context.Context, id, msg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE uploads SET status='error', error=$2, processed_at=now() WHERE id=$1`, id, msg)
	return err
}

func scanUpload(row pgx.Row) (*models.Upload, error) {
	u := &models.Upload{}
	var processedAt *time.Time
	err := row.Scan(&u.ID, &u.UserID, &u.Filename, &u.ContentType, &u.SizeBytes,
		&u.Status, &u.Error, &u.ObjectKey, &u.TxnCount, &u.CreatedAt, &processedAt)
	if err != nil {
		return nil, err
	}
	u.ProcessedAt = processedAt
	return u, nil
}

// --- transactions ---

func (s *Store) InsertTransactions(ctx context.Context, txns []*models.Transaction) error {
	if len(txns) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, t := range txns {
		batch.Queue(`
			INSERT INTO transactions (user_id, upload_id, txn_date, description, merchant, amount, direction, category)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			t.UserID, t.UploadID, t.Date, t.Description, t.Merchant, t.Amount, t.Direction, t.Category)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range txns {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

type TxnFilter struct {
	Month    string // YYYY-MM, optional
	Category string // optional
	Limit    int
}

func (s *Store) ListTransactions(ctx context.Context, userID string, f TxnFilter) ([]*models.Transaction, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, upload_id, txn_date, description, merchant, amount, direction, category, created_at
		FROM transactions
		WHERE user_id = $1
		  AND ($2 = '' OR to_char(txn_date, 'YYYY-MM') = $2)
		  AND ($3 = '' OR category = $3)
		ORDER BY txn_date DESC, created_at DESC
		LIMIT $4`, userID, f.Month, f.Category, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	txns := []*models.Transaction{}
	for rows.Next() {
		t := &models.Transaction{}
		if err := rows.Scan(&t.ID, &t.UserID, &t.UploadID, &t.Date, &t.Description,
			&t.Merchant, &t.Amount, &t.Direction, &t.Category, &t.CreatedAt); err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

// RecategorizeTransaction updates one transaction and returns its merchant so
// the caller can persist a rule from it.
func (s *Store) RecategorizeTransaction(ctx context.Context, userID, id, category string) (merchant string, err error) {
	err = s.pool.QueryRow(ctx, `
		UPDATE transactions SET category = $3
		WHERE user_id = $1 AND id = $2
		RETURNING merchant`, userID, id, category).Scan(&merchant)
	return merchant, err
}

// RecategorizeByMerchant retags every transaction whose merchant matches the
// pattern — same contains-semantics the rule engine applies at ingest.
func (s *Store) RecategorizeByMerchant(ctx context.Context, userID, pattern, category string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE transactions SET category = $3
		WHERE user_id = $1 AND merchant LIKE '%' || $2 || '%'`,
		userID, pattern, category)
	return err
}

// ClearTransactions removes all the user's uploads; their transactions are
// deleted by the ON DELETE CASCADE on transactions.upload_id. Rules remain.
func (s *Store) ClearTransactions(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM uploads WHERE user_id = $1`, userID)
	return err
}

// --- transfers ---

// TransferCandidates returns every transaction of the user in the shape the
// transfer pair matcher consumes.
func (s *Store) TransferCandidates(ctx context.Context, userID string) ([]categorize.TxnRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, upload_id, txn_date, amount, direction, category, merchant
		FROM transactions WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := []categorize.TxnRef{}
	for rows.Next() {
		var r categorize.TxnRef
		var direction string
		if err := rows.Scan(&r.ID, &r.UploadID, &r.Date, &r.Amount, &direction, &r.Category, &r.Merchant); err != nil {
			return nil, err
		}
		r.Credit = direction == string(models.Credit)
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

func (s *Store) SetTransactionsCategory(ctx context.Context, userID string, ids []string, category string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE transactions SET category = $3
		WHERE user_id = $1 AND id = ANY($2)`, userID, ids, category)
	return err
}

// --- custom categories ---

func (s *Store) ListCustomCategories(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name FROM custom_categories WHERE user_id = $1 ORDER BY lower(name)`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// AddCustomCategory inserts a category; inserted is false when a category of
// that name (case-insensitively) already exists.
func (s *Store) AddCustomCategory(ctx context.Context, userID, name string) (inserted bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO custom_categories (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, lower(name)) DO NOTHING`, userID, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// --- category rules ---

func (s *Store) UpsertRule(ctx context.Context, userID, pattern, category string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO category_rules (user_id, pattern, category) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, pattern) DO UPDATE SET category = EXCLUDED.category`,
		userID, pattern, category)
	return err
}

func (s *Store) ListRules(ctx context.Context, userID string) ([]*models.CategoryRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, pattern, category, created_at
		FROM category_rules WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []*models.CategoryRule{}
	for rows.Next() {
		r := &models.CategoryRule{}
		if err := rows.Scan(&r.ID, &r.UserID, &r.Pattern, &r.Category, &r.CreatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// --- analytics ---

func (s *Store) Summary(ctx context.Context, userID string) (*models.Summary, error) {
	sum := &models.Summary{Months: []models.MonthlyFlow{}}
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(txn_date, 'YYYY-MM') AS month,
		       COALESCE(SUM(amount) FILTER (WHERE direction = 'credit'), 0) AS inflow,
		       COALESCE(SUM(amount) FILTER (WHERE direction = 'debit'), 0)  AS outflow
		FROM transactions WHERE user_id = $1 AND category <> $2
		GROUP BY 1 ORDER BY 1`, userID, categorize.Transfers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m models.MonthlyFlow
		if err := rows.Scan(&m.Month, &m.Inflow, &m.Outflow); err != nil {
			return nil, err
		}
		sum.Months = append(sum.Months, m)
		sum.TotalInflow += m.Inflow
		sum.TotalOutflow += m.Outflow
	}
	sum.Net = sum.TotalInflow - sum.TotalOutflow
	return sum, rows.Err()
}

// CategoryBreakdown totals debit spending per category, optionally for one
// month. Transfers move money between the user's own accounts, so they are
// not spending and are excluded (as in Summary).
func (s *Store) CategoryBreakdown(ctx context.Context, userID, month string) ([]models.CategoryTotal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT category, COALESCE(SUM(amount), 0), COUNT(*)
		FROM transactions
		WHERE user_id = $1 AND direction = 'debit' AND category <> $3
		  AND ($2 = '' OR to_char(txn_date, 'YYYY-MM') = $2)
		GROUP BY category ORDER BY 2 DESC`, userID, month, categorize.Transfers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.CategoryTotal{}
	for rows.Next() {
		var c models.CategoryTotal
		if err := rows.Scan(&c.Category, &c.Total, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
