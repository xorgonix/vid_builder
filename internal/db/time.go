package db

import "time"

// Now returns the current UTC time formatted as RFC3339 with millisecond
// precision — the canonical timestamp format stored in SQLite (spec §6.5).
func Now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// NormalizeEmail lower-cases and trims an email for identity comparison.
func NormalizeEmail(e string) string {
	out := make([]byte, 0, len(e))
	for i := 0; i < len(e); i++ {
		c := e[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	s := string(out)
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
