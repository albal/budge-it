package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/budge-it/backend/internal/auth"
	"github.com/budge-it/backend/internal/config"
	"github.com/budge-it/backend/internal/models"
)

const (
	testSecret  = "test-session-secret"
	adminID     = "11111111-1111-1111-1111-111111111111"
	adminEmail  = "admin@example.com"
	victimID    = "22222222-2222-2222-2222-222222222222"
	victimEmail = "victim@example.com"
)

// fakeAdminStore is an in-memory adminStore. Each method can be made to fail
// so the handlers' error paths are exercised too.
type fakeAdminStore struct {
	users     map[string]*models.User
	listErr   error
	getErr    error
	deleteErr error
	// vanished simulates the user disappearing between the lookup and the
	// delete, i.e. two administrators removing the same account at once.
	vanished   bool
	deletedIDs []string
}

func newFakeStore() *fakeAdminStore {
	return &fakeAdminStore{users: map[string]*models.User{
		adminID:  {ID: adminID, Email: adminEmail, CreatedAt: time.Now()},
		victimID: {ID: victimID, Email: victimEmail, CreatedAt: time.Now()},
	}}
}

// withAdminFlag marks a user as having claimed the administrator seat in the
// database, as the first login does.
func (f *fakeAdminStore) withAdminFlag(id string) *fakeAdminStore {
	f.users[id].IsAdmin = true
	return f
}

func (f *fakeAdminStore) ListUsers(context.Context) ([]*models.AdminUser, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := []*models.AdminUser{}
	for _, u := range f.users {
		out = append(out, &models.AdminUser{
			ID: u.ID, Email: u.Email, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt, TxnCount: 3,
		})
	}
	return out, nil
}

func (f *fakeAdminStore) GetUserByID(_ context.Context, id string) (*models.User, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	u, ok := f.users[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return u, nil
}

func (f *fakeAdminStore) DeleteUser(_ context.Context, id string) (bool, error) {
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	if f.vanished {
		return false, nil
	}
	if _, ok := f.users[id]; !ok {
		return false, nil
	}
	delete(f.users, id)
	f.deletedIDs = append(f.deletedIDs, id)
	return true, nil
}

// newTestServer builds a Server wired to a fake store. Only the admin and auth
// surfaces are exercised, so the other collaborators stay nil.
func newTestServer(t *testing.T, adminEmails []string, fake *fakeAdminStore) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s := &Server{
		cfg:   &config.Config{SessionSecret: testSecret, AdminEmails: adminEmails},
		admin: fake,
		ready: func() error { return nil },
	}
	return s.Router()
}

// request issues a request, optionally carrying a signed session for the given
// user. Passing an empty userID sends no cookie at all.
func request(t *testing.T, r *gin.Engine, method, path, userID, email string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if userID != "" {
		req.AddCookie(&http.Cookie{
			Name:  auth.CookieName,
			Value: auth.Sign(testSecret, userID, email, time.Hour),
		})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminRoutesRejectAnonymous(t *testing.T) {
	r := newTestServer(t, []string{adminEmail}, newFakeStore())

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/users"},
		{http.MethodDelete, "/api/v1/admin/users/" + victimID},
	} {
		w := request(t, r, tc.method, tc.path, "", "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want %d", tc.method, tc.path, w.Code, http.StatusUnauthorized)
		}
	}
}

func TestAdminRoutesRejectNonAdmin(t *testing.T) {
	fake := newFakeStore()
	r := newTestServer(t, []string{adminEmail}, fake)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/users"},
		{http.MethodDelete, "/api/v1/admin/users/" + victimID},
	} {
		w := request(t, r, tc.method, tc.path, victimID, victimEmail)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s as a non-admin = %d, want %d", tc.method, tc.path, w.Code, http.StatusForbidden)
		}
	}
	// The rejected DELETE must not have touched anything.
	if len(fake.deletedIDs) != 0 {
		t.Errorf("non-admin DELETE removed users %v, want none", fake.deletedIDs)
	}
}

// With no ADMIN_EMAILS configured and no claimed flag, a valid session gets 403.
func TestAdminRoutesDenyWhenNobodyIsAdmin(t *testing.T) {
	r := newTestServer(t, nil, newFakeStore())
	w := request(t, r, http.MethodGet, "/api/v1/admin/users", adminID, adminEmail)
	if w.Code != http.StatusForbidden {
		t.Errorf("GET /admin/users with no admin at all = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// The flag claimed by the first login grants access on its own, with no
// ADMIN_EMAILS entry.
func TestAdminRoutesAllowClaimedFlag(t *testing.T) {
	r := newTestServer(t, nil, newFakeStore().withAdminFlag(adminID))
	w := request(t, r, http.MethodGet, "/api/v1/admin/users", adminID, adminEmail)
	if w.Code != http.StatusOK {
		t.Errorf("GET /admin/users as the first-login admin = %d, want %d (%s)", w.Code, http.StatusOK, w.Body)
	}
}

// Holding the flag is per-account: another user doesn't inherit it.
func TestAdminRoutesDenyOtherUserWhenFlagHeldElsewhere(t *testing.T) {
	r := newTestServer(t, nil, newFakeStore().withAdminFlag(adminID))
	w := request(t, r, http.MethodGet, "/api/v1/admin/users", victimID, victimEmail)
	if w.Code != http.StatusForbidden {
		t.Errorf("GET /admin/users as a non-claiming user = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// A session whose account has since been deleted is simply not an admin.
func TestAdminRoutesDenyDeletedAccount(t *testing.T) {
	fake := newFakeStore()
	delete(fake.users, adminID)
	r := newTestServer(t, nil, fake)

	w := request(t, r, http.MethodGet, "/api/v1/admin/users", adminID, adminEmail)
	if w.Code != http.StatusForbidden {
		t.Errorf("GET /admin/users for a deleted account = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// A lookup failure while resolving admin status is a server error, not a
// silent denial — that would be indistinguishable from a revoked flag.
func TestAdminRoutesLookupError(t *testing.T) {
	fake := newFakeStore()
	fake.getErr = errors.New("connection reset")
	r := newTestServer(t, nil, fake)

	w := request(t, r, http.MethodGet, "/api/v1/admin/users", adminID, adminEmail)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("GET /admin/users with a failing lookup = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ADMIN_EMAILS still works as the out-of-band override, without a DB flag and
// without hitting the store at all.
func TestAdminRoutesAllowlistOverridesMissingFlag(t *testing.T) {
	fake := newFakeStore()
	fake.getErr = errors.New("store must not be consulted for an allowlisted admin")
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodGet, "/api/v1/admin/users", adminID, adminEmail)
	if w.Code != http.StatusOK {
		t.Errorf("GET /admin/users as an allowlisted admin = %d, want %d (%s)", w.Code, http.StatusOK, w.Body)
	}
}

func TestListUsers(t *testing.T) {
	r := newTestServer(t, []string{adminEmail}, newFakeStore())
	w := request(t, r, http.MethodGet, "/api/v1/admin/users", adminID, adminEmail)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /admin/users = %d, want %d (%s)", w.Code, http.StatusOK, w.Body)
	}
	var users []models.AdminUser
	if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	for _, u := range users {
		if u.Email == "" || u.ID == "" {
			t.Errorf("user %+v is missing id/email", u)
		}
	}
}

func TestListUsersStoreError(t *testing.T) {
	fake := newFakeStore()
	fake.listErr = errors.New("database exploded")
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodGet, "/api/v1/admin/users", adminID, adminEmail)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("GET /admin/users with a failing store = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestDeleteUser(t *testing.T) {
	fake := newFakeStore()
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodDelete, "/api/v1/admin/users/"+victimID, adminID, adminEmail)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /admin/users/:id = %d, want %d (%s)", w.Code, http.StatusOK, w.Body)
	}
	var body struct {
		Deleted bool   `json:"deleted"`
		ID      string `json:"id"`
		Email   string `json:"email"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !body.Deleted || body.ID != victimID || body.Email != victimEmail {
		t.Errorf("response = %+v, want deleted=true id=%s email=%s", body, victimID, victimEmail)
	}
	if len(fake.deletedIDs) != 1 || fake.deletedIDs[0] != victimID {
		t.Errorf("store deletions = %v, want [%s]", fake.deletedIDs, victimID)
	}
}

// Deleting yourself would revoke your own session and, because accounts are
// created on first login, silently reappear as an empty account.
func TestDeleteUserRefusesSelf(t *testing.T) {
	fake := newFakeStore()
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodDelete, "/api/v1/admin/users/"+adminID, adminID, adminEmail)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DELETE of own account = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if len(fake.deletedIDs) != 0 {
		t.Errorf("self-delete removed %v, want nothing", fake.deletedIDs)
	}
	if _, ok := fake.users[adminID]; !ok {
		t.Error("admin account was removed despite the self-delete guard")
	}
}

func TestDeleteUserNotFound(t *testing.T) {
	r := newTestServer(t, []string{adminEmail}, newFakeStore())
	missing := "33333333-3333-3333-3333-333333333333"

	w := request(t, r, http.MethodDelete, "/api/v1/admin/users/"+missing, adminID, adminEmail)
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE of an unknown user = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteUserRejectsNonUUID(t *testing.T) {
	fake := newFakeStore()
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodDelete, "/api/v1/admin/users/not-a-uuid", adminID, adminEmail)
	if w.Code != http.StatusBadRequest {
		t.Errorf("DELETE with a malformed id = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if len(fake.deletedIDs) != 0 {
		t.Errorf("malformed id reached the store: %v", fake.deletedIDs)
	}
}

// A lookup failure that isn't "no such row" is a server error, not a 404.
func TestDeleteUserLookupError(t *testing.T) {
	fake := newFakeStore()
	fake.getErr = errors.New("connection reset")
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodDelete, "/api/v1/admin/users/"+victimID, adminID, adminEmail)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("DELETE with a failing lookup = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if len(fake.deletedIDs) != 0 {
		t.Errorf("delete proceeded despite the lookup failing: %v", fake.deletedIDs)
	}
}

// Two administrators deleting the same account concurrently: the second one
// finds the user at lookup time but deletes nothing, and should get a 404.
func TestDeleteUserRaceReportsNotFound(t *testing.T) {
	fake := newFakeStore()
	fake.vanished = true
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodDelete, "/api/v1/admin/users/"+victimID, adminID, adminEmail)
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE of a concurrently-removed user = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteUserStoreError(t *testing.T) {
	fake := newFakeStore()
	fake.deleteErr = errors.New("constraint violation")
	r := newTestServer(t, []string{adminEmail}, fake)

	w := request(t, r, http.MethodDelete, "/api/v1/admin/users/"+victimID, adminID, adminEmail)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("DELETE with a failing store = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// A session signed with the wrong secret must not authenticate, let alone
// authorize — otherwise the allowlist could be bypassed by forging an email.
func TestAdminRoutesRejectForgedSession(t *testing.T) {
	r := newTestServer(t, []string{adminEmail}, newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.AddCookie(&http.Cookie{
		Name:  auth.CookieName,
		Value: auth.Sign("attacker-secret", adminID, adminEmail, time.Hour),
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /admin/users with a forged cookie = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMeReportsAdminFlag(t *testing.T) {
	tests := []struct {
		name        string
		allowlist   []string
		claimedBy   string // user id holding the database flag, if any
		userID      string
		email       string
		wantIsAdmin bool
	}{
		{"allowlisted user", []string{adminEmail}, "", adminID, adminEmail, true},
		{"ordinary user", []string{adminEmail}, "", victimID, victimEmail, false},
		{"nobody is admin", nil, "", adminID, adminEmail, false},
		{"first-login claimant", nil, adminID, adminID, adminEmail, true},
		{"someone else claimed it", nil, adminID, victimID, victimEmail, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeStore()
			if tt.claimedBy != "" {
				fake.withAdminFlag(tt.claimedBy)
			}
			r := newTestServer(t, tt.allowlist, fake)
			w := request(t, r, http.MethodGet, "/api/v1/auth/me", tt.userID, tt.email)
			if w.Code != http.StatusOK {
				t.Fatalf("GET /auth/me = %d, want %d", w.Code, http.StatusOK)
			}
			var body struct {
				Email   string `json:"email"`
				IsAdmin bool   `json:"isAdmin"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if body.IsAdmin != tt.wantIsAdmin {
				t.Errorf("isAdmin = %v, want %v", body.IsAdmin, tt.wantIsAdmin)
			}
		})
	}
}
