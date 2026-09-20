package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

// CSRFToken derives the per-session synchronizer token exposed in forms.
func CSRFToken(secret []byte) string {
	return base64.RawURLEncoding.EncodeToString(secret)
}

// CheckCSRF constant-time compares a submitted token against the session secret.
func CheckCSRF(secret []byte, token string) bool {
	if len(secret) == 0 || token == "" {
		return false
	}
	want := CSRFToken(secret)
	if len(want) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(token)) == 1
}

// EqualToken constant-time compares two token strings.
func EqualToken(a, b string) bool {
	if a == "" || b == "" || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// NewCSRFSecret returns 32 bytes of random CSRF derivation material.
func NewCSRFSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("auth: crypto/rand unavailable: " + err.Error())
	}
	return b
}
