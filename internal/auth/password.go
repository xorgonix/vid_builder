// Package auth implements password hashing (Argon2id) and session tokens.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// HashParams are the Argon2id parameters used for new hashes. Existing hashes
// carry their own encoded parameters, so changes do not invalidate old hashes.
type HashParams struct {
	MemoryKB   uint32
	Iterations uint32
	ParallelIn uint8
	SaltLen    int
	KeyLen     int
}

func DefaultParams(memoryKB, iterations, parallelism int) HashParams {
	return HashParams{
		MemoryKB:   uint32(memoryKB),
		Iterations: uint32(iterations),
		ParallelIn: uint8(parallelism),
		SaltLen:    16,
		KeyLen:     32,
	}
}

// HashPassword derives an encoded Argon2id hash ("$argon2id$v=19$m=...,t=...,p=...$salt$hash").
func HashPassword(password string, p HashParams) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKB, p.ParallelIn, uint32(p.KeyLen))
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKB, p.Iterations, p.ParallelIn,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against an encoded Argon2id hash using
// constant-time comparison of the derived key.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var mem, iter uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iter, mem, par, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken returns a random URL-safe token (raw) and its SHA-256 hash for storage.
func NewToken() (token string, hash [32]byte) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic("auth: crypto/rand unavailable: " + err.Error())
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	hash = sha256.Sum256([]byte(token))
	return token, hash
}

// HashToken returns the SHA-256 hash of a token as bytes for storage lookup.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
