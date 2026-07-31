package store

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/budge-it/backend/internal/db"
)

// newTestStore connects to the database named by TEST_DATABASE_URL, applies
// the migrations and hands back an empty users table. Tests are skipped when
// the variable is unset so `go test ./...` still works without a database.
//
//	make test-integration    # brings up docker compose and sets the variable
func newTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database integration test")
	}
	ctx := context.Background()
	poolCfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parsing TEST_DATABASE_URL: %v", err)
	}
	// The concurrency test needs its goroutines genuinely in flight at once;
	// a small pool would serialize them and hide a race.
	poolCfg.MaxConns = 24
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pinging test database: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}
	t.Cleanup(pool.Close)

	truncate := func() {
		if _, err := pool.Exec(ctx, `TRUNCATE users CASCADE`); err != nil {
			t.Fatalf("truncating users: %v", err)
		}
	}
	// Start from a clean slate (this also drops the user seeded by 001) and
	// leave one behind for the next run.
	truncate()
	t.Cleanup(truncate)

	return New(pool)
}

func adminCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM users WHERE is_admin`).Scan(&n); err != nil {
		t.Fatalf("counting administrators: %v", err)
	}
	return n
}

// The first account to log in claims the administrator seat; later ones don't.
func TestFirstLoginClaimsAdmin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.GetOrCreateUserByEmail(ctx, "first@example.com")
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if !first.IsAdmin {
		t.Error("first user to log in is not an administrator, want administrator")
	}

	second, err := s.GetOrCreateUserByEmail(ctx, "second@example.com")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if second.IsAdmin {
		t.Error("second user to log in is an administrator, want not")
	}

	if got := adminCount(t, s); got != 1 {
		t.Errorf("administrator count = %d, want 1", got)
	}
}

// Logging in again must report the flag, not re-run the claim.
func TestRepeatLoginKeepsAdminFlag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.GetOrCreateUserByEmail(ctx, "first@example.com")
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	again, err := s.GetOrCreateUserByEmail(ctx, "first@example.com")
	if err != nil {
		t.Fatalf("repeat login: %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("repeat login created a new account: %s != %s", again.ID, first.ID)
	}
	if !again.IsAdmin {
		t.Error("repeat login lost the administrator flag")
	}
	if got := adminCount(t, s); got != 1 {
		t.Errorf("administrator count = %d, want 1", got)
	}
}

// An existing non-admin user logging in when the seat is still free claims it
// — this is the path an already-populated database takes after migration 003.
func TestExistingUserClaimsFreeSeat(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Simulate a pre-existing account by clearing the flag after creation.
	if _, err := s.GetOrCreateUserByEmail(ctx, "existing@example.com"); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET is_admin = false`); err != nil {
		t.Fatalf("clearing flag: %v", err)
	}

	u, err := s.GetOrCreateUserByEmail(ctx, "existing@example.com")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !u.IsAdmin {
		t.Error("existing user did not claim the free administrator seat")
	}
}

// The claim must be settled under concurrency: with many simultaneous first
// logins exactly one may come away as administrator.
//
// The goroutines are held at a barrier and released together so they all
// observe the seat as free before any of them commits — that is the window
// the advisory lock exists to close. Removing the lock from
// createAndMaybeClaimAdmin makes this test fail with several winners; it was
// verified to do so, because a concurrency test that passes either way is
// worth nothing. Several rounds are run since one round can get lucky.
func TestConcurrentFirstLoginsProduceOneAdmin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const (
		rounds = 5
		racers = 16
	)
	for round := range rounds {
		if _, err := s.pool.Exec(ctx, `TRUNCATE users CASCADE`); err != nil {
			t.Fatalf("resetting users: %v", err)
		}

		var (
			start   = make(chan struct{})
			wg      sync.WaitGroup
			mu      sync.Mutex
			winners []string
			errs    []error
		)
		wg.Add(racers)
		for i := range racers {
			go func() {
				defer wg.Done()
				<-start // release everyone at once
				u, err := s.GetOrCreateUserByEmail(ctx,
					fmt.Sprintf("r%d-racer%d@example.com", round, i))
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
					return
				}
				if u.IsAdmin {
					winners = append(winners, u.Email)
				}
			}()
		}
		close(start)
		wg.Wait()

		for _, err := range errs {
			t.Errorf("round %d: concurrent login failed: %v", round, err)
		}
		if len(winners) != 1 {
			t.Fatalf("round %d: %d logins reported administrator (%v), want exactly 1",
				round, len(winners), winners)
		}
		if got := adminCount(t, s); got != 1 {
			t.Fatalf("round %d: administrator rows = %d, want 1", round, got)
		}
	}
}

// Deleting an account removes its data and reports whether anything went.
func TestDeleteUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.GetOrCreateUserByEmail(ctx, "doomed@example.com")
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	if err := s.UpsertRule(ctx, u.ID, "tesco", "Groceries"); err != nil {
		t.Fatalf("creating rule: %v", err)
	}
	if _, err := s.AddCustomCategory(ctx, u.ID, "Hobbies"); err != nil {
		t.Fatalf("creating category: %v", err)
	}

	deleted, err := s.DeleteUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteUser reported nothing deleted, want deleted")
	}

	leftovers := []struct{ table, column string }{
		{"users", "id"},
		{"uploads", "user_id"},
		{"transactions", "user_id"},
		{"category_rules", "user_id"},
		{"custom_categories", "user_id"},
	}
	for _, l := range leftovers {
		var n int
		// Table and column are fixed literals above, not user input.
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = $1`, l.table, l.column)
		if err := s.pool.QueryRow(ctx, q, u.ID).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", l.table, err)
		}
		if n != 0 {
			t.Errorf("%d rows left in %s after deleting the user, want 0", n, l.table)
		}
	}
}

func TestDeleteUserUnknownID(t *testing.T) {
	s := newTestStore(t)
	deleted, err := s.DeleteUser(context.Background(), "00000000-0000-0000-0000-0000000000ff")
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if deleted {
		t.Error("DeleteUser reported a deletion for an unknown id")
	}
}

// ListUsers reports per-user counts, including zeroes for an empty account.
func TestListUsersCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.GetOrCreateUserByEmail(ctx, "counted@example.com")
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	if err := s.UpsertRule(ctx, u.ID, "tesco", "Groceries"); err != nil {
		t.Fatalf("creating rule: %v", err)
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1", len(users))
	}
	got := users[0]
	if got.Email != "counted@example.com" {
		t.Errorf("email = %q, want counted@example.com", got.Email)
	}
	if got.RuleCount != 1 {
		t.Errorf("ruleCount = %d, want 1", got.RuleCount)
	}
	if got.TxnCount != 0 || got.UploadCount != 0 || got.CategoryCount != 0 {
		t.Errorf("expected zero txn/upload/category counts, got %+v", got)
	}
	if !got.IsAdmin {
		t.Error("the only user should hold the administrator flag")
	}
}
