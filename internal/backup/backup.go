// Package backup creates coordinated SQLite + media-manifest backups (spec §20).
// A database-only copy is never labelled complete: the manifest records every
// media URI with its checksum so a restore can be verified.
package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MediaEntry references one stored media file.
type MediaEntry struct {
	URI       string `json:"uri"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// Manifest describes a backup set.
type Manifest struct {
	Schema     string       `json:"schema"`
	Version    int          `json:"version"`
	CreatedAt  string       `json:"created_at"`
	Database   string       `json:"database"`
	MediaCount int          `json:"media_count"`
	Complete   bool         `json:"complete"`
	Media      []MediaEntry `json:"media"`
	Note       string       `json:"note"`
}

// Create writes a consistent database copy and a media manifest into a new
// timestamped directory under backupsDir, returning the directory path.
func Create(ctx context.Context, db *sql.DB, media []MediaEntry, backupsDir string) (string, error) {
	ts := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Join(backupsDir, "backup-"+ts)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("backup: mkdir: %w", err)
	}
	dbPath := filepath.Join(dir, "vidbuild.db")

	// VACUUM INTO produces a consistent copy without holding a long transaction.
	quoted := "'" + strings.ReplaceAll(filepath.ToSlash(dbPath), "'", "''") + "'"
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		return "", fmt.Errorf("backup: vacuum into: %w", err)
	}

	manifest := Manifest{
		Schema:     "vid-build-backup",
		Version:    1,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Database:   "vidbuild.db",
		MediaCount: len(media),
		Complete:   true,
		Media:      media,
		Note:       "Database copy plus media manifest (URIs and SHA-256). Media files remain in the media store; verify before restoring.",
	}
	if manifest.Media == nil {
		manifest.Media = []MediaEntry{}
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// VerifyResult reports a backup's integrity.
type VerifyResult struct {
	Dir        string
	DBOK       bool
	DBError    string
	MediaTotal int
	MediaOK    int
	MediaBad   []string
}

// Verify checks the database copy and re-hashes every referenced media file.
func Verify(ctx context.Context, dir, mediaRoot string) (*VerifyResult, error) {
	res := &VerifyResult{Dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("backup: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("backup: parse manifest: %w", err)
	}

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, m.Database))+"?mode=ro")
	if err != nil {
		res.DBError = err.Error()
	} else {
		var ic string
		if qerr := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&ic); qerr != nil {
			res.DBError = qerr.Error()
		} else if ic != "ok" {
			res.DBError = ic
		} else {
			res.DBOK = true
		}
		db.Close()
	}

	res.MediaTotal = len(m.Media)
	for _, e := range m.Media {
		full := filepath.Join(mediaRoot, filepath.FromSlash(e.URI))
		f, err := os.Open(full)
		if err != nil {
			res.MediaBad = append(res.MediaBad, e.URI+" (missing)")
			continue
		}
		h := sha256.New()
		_, cerr := io.Copy(h, f)
		f.Close()
		if cerr != nil {
			res.MediaBad = append(res.MediaBad, e.URI+" (read error)")
			continue
		}
		if hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
			res.MediaBad = append(res.MediaBad, e.URI+" (checksum mismatch)")
			continue
		}
		res.MediaOK++
	}
	return res, nil
}

// Entry is a backup directory summary.
type Entry struct {
	Name      string
	CreatedAt string
	SizeBytes int64
	Complete  bool
	Media     int
}

// List returns backups newest first.
func List(backupsDir string) ([]Entry, error) {
	des, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		if !de.IsDir() {
			continue
		}
		dir := filepath.Join(backupsDir, de.Name())
		e := Entry{Name: de.Name()}
		if raw, err := os.ReadFile(filepath.Join(dir, "manifest.json")); err == nil {
			var m Manifest
			if json.Unmarshal(raw, &m) == nil {
				e.CreatedAt = m.CreatedAt
				e.Complete = m.Complete
				e.Media = m.MediaCount
			}
		}
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if fi, fierr := d.Info(); fierr == nil {
					e.SizeBytes += fi.Size()
				}
			}
			return nil
		})
		out = append(out, e)
	}
	// Newest first by name (timestamped).
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Name > out[i].Name {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}
