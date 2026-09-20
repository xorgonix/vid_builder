// Command vidbuild-restore restores a vid-build backup written by the backup
// job. It is deliberately an offline tool: stop the application first. It
// verifies the backup, keeps a safety copy of the current database, and swaps
// the database file. Media files live in the media store and are verified
// against the backup manifest rather than copied.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"vidbuild/internal/backup"
)

func main() {
	backupDir := flag.String("backup", "", "path to a backup directory (data/backups/backup-YYYYMMDD-HHMMSS)")
	mediaRoot := flag.String("media", "data/media", "media store root to verify against the manifest")
	dbPath := flag.String("db", "data/vidbuild.db", "target database path to restore into")
	yes := flag.Bool("yes", false, "confirm the swap (otherwise this is a dry run)")
	flag.Parse()

	if *backupDir == "" {
		fmt.Fprintln(os.Stderr, "usage: vidbuild-restore -backup <dir> [-media data/media] [-db data/vidbuild.db] [-yes]")
		os.Exit(2)
	}

	ctx := context.Background()
	res, err := backup.Verify(ctx, *backupDir, *mediaRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify failed:", err)
		os.Exit(1)
	}
	fmt.Printf("backup:      %s\n", filepath.Base(*backupDir))
	fmt.Printf("database:    integrity %s\n", okText(res.DBOK, res.DBError))
	fmt.Printf("media:       %d/%d checksums match\n", res.MediaOK, res.MediaTotal)
	if len(res.MediaBad) > 0 {
		fmt.Printf("media issues: %v\n", res.MediaBad)
	}
	if !res.DBOK {
		fmt.Fprintln(os.Stderr, "refusing to restore a database that fails integrity check")
		os.Exit(1)
	}
	if !*yes {
		fmt.Println("\ndry run: re-run with -yes to restore (stop the application first)")
		return
	}

	// Locate the backup database file.
	dbName := "vidbuild.db"
	if _, err := os.Stat(filepath.Join(*backupDir, dbName)); err != nil {
		fmt.Fprintln(os.Stderr, "backup database not found:", err)
		os.Exit(1)
	}

	// Safety copy of the current database.
	if _, err := os.Stat(*dbPath); err == nil {
		safe := fmt.Sprintf("%s.pre-restore-%s", *dbPath, time.Now().UTC().Format("20060102-150405"))
		if err := copyFile(*dbPath, safe); err != nil {
			fmt.Fprintln(os.Stderr, "safety copy failed:", err)
			os.Exit(1)
		}
		fmt.Printf("safety copy: %s\n", safe)
	}

	// Atomic swap: copy to a temp file in the target directory, then rename.
	dir := filepath.Dir(*dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	tmp := filepath.Join(dir, ".restore-"+time.Now().UTC().Format("20060102-150405")+".db")
	if err := copyFile(filepath.Join(*backupDir, dbName), tmp); err != nil {
		fmt.Fprintln(os.Stderr, "copy backup db:", err)
		os.Exit(1)
	}
	// Remove stale WAL/SHM so SQLite does not mix old journals with the new file.
	_ = os.Remove(*dbPath + "-wal")
	_ = os.Remove(*dbPath + "-shm")
	if err := os.Rename(tmp, *dbPath); err != nil {
		fmt.Fprintln(os.Stderr, "replace database:", err)
		os.Exit(1)
	}

	// Confirm the restored database opens and passes integrity.
	d, err := sql.Open("sqlite", "file:"+filepath.ToSlash(*dbPath)+"?mode=ro")
	if err != nil {
		fmt.Fprintln(os.Stderr, "open restored db:", err)
		os.Exit(1)
	}
	defer d.Close()
	var ic string
	if err := d.QueryRow(`PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		fmt.Fprintf(os.Stderr, "restored database integrity check: %v %s\n", err, ic)
		os.Exit(1)
	}
	fmt.Println("\nrestore complete. Start the application; media remains in the media store.")
}

func okText(ok bool, reason string) string {
	if ok {
		return "ok"
	}
	return "FAILED: " + reason
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
