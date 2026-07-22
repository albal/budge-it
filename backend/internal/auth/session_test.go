package auth

import (
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	token := Sign("s3cret", "user-1", "person@example.com", time.Hour)
	userID, email, err := Verify("s3cret", token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if userID != "user-1" || email != "person@example.com" {
		t.Fatalf("Verify() = %q, %q, want user-1, person@example.com", userID, email)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	token := Sign("s3cret", "user-1", "person@example.com", time.Hour)
	if _, _, err := Verify("wrong-secret", token); err == nil {
		t.Fatal("Verify() with wrong secret should fail")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	token := Sign("s3cret", "user-1", "person@example.com", -time.Hour)
	if _, _, err := Verify("s3cret", token); err == nil {
		t.Fatal("Verify() with expired token should fail")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if _, _, err := Verify("s3cret", "not-a-token"); err == nil {
		t.Fatal("Verify() with malformed token should fail")
	}
}
