// Package media stores generation/production files outside SQLite and returns
// project-relative forward-slash URIs (spec §20). File placement is atomic:
// write temp, checksum, rename, then the caller commits database rows.
package media

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vidbuild/internal/ids"
)

// Store manages the media root.
type Store struct {
	Root string
}

// New constructs a media store rooted at root.
func New(root string) *Store { return &Store{Root: root} }

// Put writes r to relDir under the media root and returns the relative URI,
// SHA-256 checksum and byte size.
func (m *Store) Put(relDir, filename string, r io.Reader) (uri, checksum string, size int64, err error) {
	relDir = filepath.ToSlash(relDir)
	relDir = strings.TrimPrefix(relDir, "/")
	dir := filepath.Join(m.Root, filepath.FromSlash(relDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, fmt.Errorf("media: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("media: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		tmp.Close()
		return "", "", 0, fmt.Errorf("media: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", "", 0, err
	}
	checksum = hex.EncodeToString(h.Sum(nil))
	if filename == "" {
		filename = ids.New() + ".bin"
	}
	// Prefix with a short checksum to make duplicate detection visible and
	// avoid collisions between identical filenames.
	filename = checksum[:8] + "-" + filename
	final := filepath.Join(dir, filename)
	if err := os.Rename(tmpName, final); err != nil {
		return "", "", 0, fmt.Errorf("media: rename: %w", err)
	}
	uri = relDir + "/" + filename
	return uri, checksum, size, nil
}

// Resolve returns an absolute path for a stored URI, rejecting traversal.
func (m *Store) Resolve(uri string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(uri))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("media: unsafe uri %q", uri)
	}
	full := filepath.Join(m.Root, clean)
	rootAbs, err := filepath.Abs(m.Root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(fullAbs, rootAbs+string(os.PathSeparator)) && fullAbs != rootAbs {
		return "", fmt.Errorf("media: uri escapes root %q", uri)
	}
	return fullAbs, nil
}

// Open opens a stored asset for reading.
func (m *Store) Open(uri string) (*os.File, error) {
	p, err := m.Resolve(uri)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Delete removes a stored file.
func (m *Store) Delete(uri string) error {
	p, err := m.Resolve(uri)
	if err != nil {
		return err
	}
	return os.Remove(p)
}
