// Package auth implements stateless, HMAC-signed session tokens. There is no
// password: a session simply attests "this request was issued a cookie after
// providing this email address." Signing (rather than a server-side session
// table) means any backend replica can verify a cookie without shared state.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CookieName is the name of the session cookie set on login.
const CookieName = "budgeit_session"

// TTL is how long a session cookie remains valid.
const TTL = 30 * 24 * time.Hour

// Sign builds a signed session token for the given user.
func Sign(secret, userID, email string, ttl time.Duration) string {
	payload := encode(userID) + "." + encode(email) + "." + strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	return payload + "." + sign(secret, payload)
}

// Verify checks a session token's signature and expiry, returning the
// embedded user ID and email if valid.
func Verify(secret, token string) (userID, email string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 {
		return "", "", fmt.Errorf("malformed session token")
	}
	payload := parts[0] + "." + parts[1] + "." + parts[2]
	if !hmac.Equal([]byte(sign(secret, payload)), []byte(parts[3])) {
		return "", "", fmt.Errorf("invalid session signature")
	}
	expiry, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", "", fmt.Errorf("malformed session expiry")
	}
	if time.Now().Unix() > expiry {
		return "", "", fmt.Errorf("session expired")
	}
	userID, err = decode(parts[0])
	if err != nil {
		return "", "", err
	}
	email, err = decode(parts[1])
	if err != nil {
		return "", "", err
	}
	return userID, email, nil
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func decode(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("malformed session field")
	}
	return string(b), nil
}
