package web

import (
	"net/url"
	"strconv"
	"strings"
)

// urlMsg encodes a one-off flash message for a redirect query string.
func urlMsg(msg string) string {
	return url.QueryEscape(msg)
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
