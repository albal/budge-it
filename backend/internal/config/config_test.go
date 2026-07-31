package config

import "testing"

func TestIsAdmin(t *testing.T) {
	cfg := &Config{AdminEmails: []string{"boss@example.com", "ops@example.com"}}

	tests := []struct {
		name  string
		email string
		want  bool
	}{
		{"listed address", "boss@example.com", true},
		{"second listed address", "ops@example.com", true},
		{"different case", "BOSS@Example.COM", true},
		{"surrounding whitespace", "  boss@example.com  ", true},
		{"not listed", "someone@example.com", false},
		{"empty", "", false},
		// Guards against a substring match creeping into the comparison.
		{"substring of a listed address", "boss@example.co", false},
		{"listed address as substring", "notboss@example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.IsAdmin(tt.email); got != tt.want {
				t.Errorf("IsAdmin(%q) = %v, want %v", tt.email, got, tt.want)
			}
		})
	}
}

// An empty allowlist must deny everyone — the admin endpoints are opt-in.
func TestIsAdminEmptyAllowlistDeniesAll(t *testing.T) {
	cfg := &Config{}
	for _, email := range []string{"", "anyone@example.com", "root@localhost"} {
		if cfg.IsAdmin(email) {
			t.Errorf("IsAdmin(%q) = true with no ADMIN_EMAILS set, want false", email)
		}
	}
}

func TestEnvListParsing(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"unset", "", nil},
		{"single", "a@x.com", []string{"a@x.com"}},
		{"multiple", "a@x.com,b@x.com", []string{"a@x.com", "b@x.com"}},
		{"whitespace and empties", " a@x.com , ,b@x.com ,", []string{"a@x.com", "b@x.com"}},
		{"uppercase normalized", "A@X.com", []string{"a@x.com"}},
		{"only separators", ",,,", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_ADMIN_LIST", tt.raw)
			got := envList("TEST_ADMIN_LIST")
			if len(got) != len(tt.want) {
				t.Fatalf("envList(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("envList(%q) = %v, want %v", tt.raw, got, tt.want)
				}
			}
		})
	}
}

// Load must wire ADMIN_EMAILS through to a working IsAdmin.
func TestLoadReadsAdminEmails(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("SESSION_SECRET", "test-secret")
	t.Setenv("ADMIN_EMAILS", "Admin@Example.com, second@example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.IsAdmin("admin@example.com") {
		t.Error("IsAdmin(admin@example.com) = false, want true")
	}
	if !cfg.IsAdmin("second@example.com") {
		t.Error("IsAdmin(second@example.com) = false, want true")
	}
	if cfg.IsAdmin("third@example.com") {
		t.Error("IsAdmin(third@example.com) = true, want false")
	}
}
