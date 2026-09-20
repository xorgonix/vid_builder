package web

import (
	"database/sql"
	"net/url"
	"strconv"
	"strings"
)

// urlMsg encodes a one-off flash message for a redirect query string.
func urlMsg(msg string) string {
	return url.QueryEscape(msg)
}

// nullString wraps a value as a valid sql.NullString.
func nullString(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}

// itoa renders an int as a string.
func itoa(n int) string { return strconv.Itoa(n) }

// withQuery appends a query string to a URL, choosing ? or & as needed.
func withQuery(base, query string) string {
	if query == "" {
		return base
	}
	if strings.Contains(base, "?") {
		return base + "&" + query
	}
	return base + "?" + query
}

// flashQuery builds a msg/kind query string.
func flashQuery(msg, kind string) string {
	return "msg=" + urlMsg(msg) + "&kind=" + kind
}

// parseInt parses an int with a fallback.
func parseInt(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
