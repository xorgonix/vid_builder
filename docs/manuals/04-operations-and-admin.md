# vid-build 0.02 — Operations and administration

*Protect the work, reach it from the couch, and keep it running.*

This manual covers **project packages**, **backup / verify / restore**, **LAN
access, the installable shell and offline drafts**, **settings and local
services**, **diagnostics**, **security and privacy**, the **`config.json`
reference**, the **command-line reference**, **troubleshooting**, **limitations**
and the **glossary**.

---

## Contents

1. [Project packages](#1-project-packages)
2. [Backup, verify and restore](#2-backup-verify-and-restore)
3. [LAN access — phone and tablet](#3-lan-access--phone-and-tablet)
4. [Installable shell and offline drafts](#4-installable-shell-and-offline-drafts)
5. [Settings and local services](#5-settings-and-local-services)
6. [Diagnostics reference](#6-diagnostics-reference)
7. [Security and privacy](#7-security-and-privacy)
8. [config.json reference](#8-configjson-reference)
9. [Command-line reference](#9-command-line-reference)
10. [Troubleshooting](#10-troubleshooting)
11. [Limitations and not-yet-implemented](#11-limitations-and-not-yet-implemented)
12. [Glossary](#12-glossary)

---

## 1. Project packages

**Project → Package** (`/w/{workspace}/projects/{project}/package`) previews a
portable export: counts of videos, sections, scenes, shots, bible entries,
briefs, proposals and media files. **Queue portable package** builds a ZIP
containing:

- `project.json` — schema version and all project rows;
- `manifest.json` — the media list with checksums;
- `media/…` — the referenced files.

Download the latest package from the same page.

---

## 2. Backup, verify and restore

### 2.1 Creating a backup

**Workspace → Diagnostics → Backups → Queue backup** (owners). A backup is:

- a **consistent database copy** (SQLite `VACUUM INTO`), and
- a **media manifest** listing every media URI with its SHA-256.

A database-only copy is never labelled complete.

### 2.2 Verifying

**Verify** re-checks the database integrity and **re-hashes every referenced
media file**. The result reports e.g. *"database integrity ok, 12/12 media
checksums match"*, or names the first file that is missing or changed.

### 2.3 Restoring (offline)

Restore is a command-line tool — stop the application first:

```bash
# dry run: verify the backup and report, change nothing
go run ./cmd/vidbuild-restore -backup data/backups/backup-YYYYMMDD-HHMMSS

# perform the swap: keeps a safety copy of the current database, then replaces it
go run ./cmd/vidbuild-restore -backup data/backups/backup-YYYYMMDD-HHMMSS -yes
```

Restore verifies the backup, copies the current database aside
(`*.pre-restore-*`), atomically swaps in the backup, and confirms integrity.
Media files stay in the media store and are verified against the manifest.

> **Migrations are forward-only.** Applying a newer build to a database upgrades
> its schema in place. Take a backup before upgrading; the app records each
> migration's checksum and refuses to run a database whose applied migrations
> have changed.

---

## 3. LAN access — phone and tablet

**Workspace → Settings → LAN access** (owners). LAN is **off by default**.

1. **Enable LAN access** — starts a separate listener (`lan_bind_host`:`lan_port`)
   and issues a **pairing code** valid for ten minutes, **single use**.
2. On the phone, open the shown address and enter the code (or open the pairing
   link). The device receives its **own session**, separate from your desktop.
3. Manage or revoke devices under **Account → Active sessions**.
4. **Disable and revoke** closes the listener and invalidates outstanding codes.

The device talks only to vid-build. It never receives ComfyUI credentials or
filesystem paths, and local ComfyUI is never exposed.

---

## 4. Installable shell and offline drafts

The web interface is an installable PWA (web manifest, icons, service worker).
The service worker caches only the app shell and safe static assets; it never
caches mutations, generated media, playback data or job polling.

When the desktop is unreachable, **safe creative drafts** are queued on the
device in an IndexedDB store: concept notes, annotations, shot text edits and
blocking drafts. They sync automatically when the connection returns, or from
**Offline drafts** (`.../drafts`) via **Sync now**.

- Shot text edits carry the base revision. If the shot changed on the server
  meanwhile, you get a **three-way conflict view** (base / server current / your
  draft) instead of an overwrite.
- Everything else — production, approvals, deletions, membership and workflow
  configuration — requires a live server connection and is refused offline.

---

## 5. Settings and local services

**Local services** (`/settings/local-services`):

- **Probe all services** — checks LM Studio (and names the model), ComfyUI
  (version and VRAM) and FFmpeg (on `PATH`), and reports the results.
- **Memory coordination** — see the configured unload command and **Unload
  planner now**. Before local GPU work the worker runs the unload command to free
  the planner's VRAM, records the action (or failure) as a job event, and reloads
  after work if configured.

**Workspace settings** (`/w/{workspace}/settings`) gathers identity,
**Membership**, **Execution targets**, **Model profiles** and **LAN access**.

---

## 6. Diagnostics reference

**Workspace → Diagnostics** (`/w/{workspace}/diagnostics`) reports:

| Reading | Meaning |
|---|---|
| Jobs queued / running / failed | worker health |
| SQLite integrity | `ok` or the failure |
| Foreign-key violations | should be 0 |
| Journal mode | `wal` |
| Media root / assets / attempts | storage overview |
| Working directory / platform | environment |
| Backups + Verify | §2 |
| **Generate missing thumbnails** | derive previews for existing image assets |

---

## 7. Security and privacy

- **Passwords** are stored as Argon2id hashes.
- **Sessions** are opaque tokens; only a SHA-256 hash is stored. Every mutation
  requires a **CSRF** token.
- **Authorization** is enforced in the services and is **workspace-scoped** — a
  project, asset or job never crosses a workspace boundary without permission.
- **Local services** are reached over loopback only; ComfyUI is never
  reverse-proxied.
- **LAN access** is opt-in, paired, per-device revocable, and disabled by
  default.
- **Secrets** never enter the database, HTML, job payloads or generation
  receipts.
- Every consequential action records the **human actor** in the audit log.
- **Guided tours** use your session and the same permission and CSRF checks as
  the normal interface; they cannot do anything you could not do by hand, and
  they never queue generation.

---

## 8. config.json reference

`config.json` is gitignored; `config.example.json` is the committed template.

```jsonc
{
  "server":   { "host": "127.0.0.1", "port": 8971, "mode": "local-web",
                "bootstrap_token_ttl_minutes": 10,
                "lan_bind_host": "0.0.0.0", "lan_port": 8972 },
  "database": { "path": "data/vidbuild.db", "busy_timeout_ms": 5000, "max_pool_conns": 8 },
  "media":    { "root_dir": "data/media", "temp_dir": "data/temp",
                "thumbnail_dir": "data/media/thumbs", "exports_dir": "data/exports" },
  "llm":      { "enabled": true, "base_url": "http://127.0.0.1:1234",
                "model": "qwen3.5-9b-uncensored-hauhaucs-aggressive",
                "api_key": "", "temperature": 0.7, "max_tokens": 8192,
                "request_timeout_seconds": 180, "planner_system_prompt": "…",
                "unload_before_gpu": true, "unload_command": "lms unload --all",
                "reload_command": "" },
  "comfyui":  { "enabled": true, "base_url": "http://127.0.0.1:8188",
                "request_timeout_seconds": 30, "poll_interval_ms": 1000 },
  "ffmpeg":   { "enabled": true, "binary_path": "ffmpeg", "request_timeout_seconds": 600 },
  "auth":     { "session_ttl_hours": 720, "argon2_memory_kb": 65536,
                "argon2_iterations": 2, "argon2_parallelism": 2 },
  "security": { "cookie_secure": false, "cookie_name": "vidbuild_session",
                "rate_limit_per_minute": 20 },
  "worker":   { "poll_interval_ms": 1500, "lease_ttl_seconds": 120, "max_attempts": 3 },
  "logging":  { "level": "info", "file": "data/logs/vidbuild.log", "console": true },
  "bootstrap":{ "admin_email": "admin@vidbuild.local", "admin_password": "change-me",
                "admin_name": "Studio Admin" }
}
```

Key notes:

- `bootstrap` seeds the first administrator **only when no users exist**.
- `llm.unload_before_gpu` + `llm.unload_command` implement memory coordination
  (§5).
- `security.cookie_secure` should be `true` behind HTTPS; keep `false` for plain
  local HTTP.
- `server.mode` is informational metadata (`desktop` / `local-web` / `lan` /
  `server`); LAN availability is controlled at runtime from the UI.

---

## 9. Command-line reference

```bash
# regenerate templates (after editing any .templ file)
templ generate

# build the server binary
go build -o bin/vidbuild.exe ./cmd/vidbuild      # Git Bash uses ./ ; PowerShell .\

# run
go run ./cmd/vidbuild                 # http://127.0.0.1:8971
go run ./cmd/vidbuild -open           # also opens the browser
go run ./cmd/vidbuild -config my.json

# checks
go test ./...
go vet ./...

# restore (offline)
go run ./cmd/vidbuild-restore -backup data/backups/backup-YYYYMMDD-HHMMSS
go run ./cmd/vidbuild-restore -backup data/backups/backup-YYYYMMDD-HHMMSS -yes
```

---

## 10. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `no Go files in D:\Dev\vid_builder` | You ran `go build` at the root. Use `go build ./cmd/vidbuild`. |
| `no required module provides package .cmdvidbuild` | Git Bash ate the backslashes. Use `./cmd/vidbuild`. |
| Sign-in fails | Check the `bootstrap` credentials in `config.json`; the account exists only if a user was created. |
| Planner buttons disabled / "Planner unavailable" | LM Studio not reachable or `llm.enabled=false`. Check Local services → Probe. |
| "No validated workflow available" | Import a workflow and **Validate** it (or use a bundled package). |
| Generate button disabled | ComfyUI unreachable. Start it and reload the form. |
| "ComfyUI unreachable" during a job | The worker reports the preflight failure; the request is marked failed. |
| Motion refused | Select **and approve** both endpoints, and choose a workflow that declares first/last bindings. |
| Super Shot rejected | The shot needs an approved start and end image, and the workflow must declare first/last bindings and a `video_timing` capability block (frame rule, frame bounds, supported FPS). |
| A Super Shot segment failed | Only that segment failed; press **Retry** on that row. The other segments are untouched. |
| The assembled Super Shot is missing | It appears only once every segment has a current output and the assembly's measured duration matches the plan. Retry any failed or stale segment. |
| "Cannot demote/remove the last owner" | Add another owner before changing yours. |
| 403 on submit | Session expired or CSRF mismatch — reload and sign in again. |
| "Section/Scene/Shot changed elsewhere" | Optimistic revision conflict; the page reloaded with the current data. |
| Export fails | FFmpeg missing from `PATH`, or a clip's asset is missing. Run Diagnostics. |
| A guided tour stops mid-step | A control was not on the page. Open the screen the caption names, then press **Next**. |
| Video workflow crashes ComfyUI with `hostbuf_file_reader_read failed` or a Windows access violation during weight load | An environment-side weight-loading fault, not a vid-build graph fault. The bundled Wan package is verified on ComfyUI 0.35.0. Update ComfyUI and its `comfy-aimdo` package, retry once with a smaller resolution/length, and ensure the model files are complete. |
| Data looks empty | You may be in the wrong workspace (see the switcher) or signed in as a different user. |

---

## 11. Limitations and not-yet-implemented

vid-build 0.02 is a working vertical slice. Known gaps:

- **Wails packaging and signing** — the app runs as a local web app; the Wails
  desktop host is scaffolded conceptually but not packaged.
- **Rental/commercial API adapters** — targets can be recorded and campaigns
  planned, but only the local ComfyUI target executes today.
- **Global keyboard shortcuts** (e.g. `g d`, `/` search) are specified but not
  yet bound.
- **Timeline drag** — ordering uses Move Up/Down and numeric trims; scrubbing is
  not yet a drag surface.
- **Model-based asset assessment** — human assessment works; automated model
  assessment is not wired to a vision model.
- **Audio** — audio intent is modelled on the brief; there is no audio generation
  yet.
- **Vector search** — not implemented.
- **Restore UI** — restore is a CLI tool by design (safer than a live in-app
  swap).
- **Arbitrary durations** — duration video supports exactly 5, 15 and 30
  seconds.
- **Advanced transitions** — duration-video segments are joined with minimal
  cuts; there are no cross-segment effects.
- **Recorded screencasts** — the guided tours drive the live interface rather
  than playing video files.

---

## 12. Glossary

**Anchor** — one ordered boundary state in a Super Shot: the supplied start, an intermediate, or the supplied end. Adjacent segments share their boundary anchor and, once rendered, its exact image.

**Asset** — a produced file (image, video, document, export, thumbnail), stored
on disk with a checksum.

**Attempt** — one immutable execution of a generation request for one candidate.

**Binding** — a confirmed mapping from a semantic field to a ComfyUI node input.

**Blocking** — structured JSON describing composition/pose/camera, rendered as
SVG.

**Brief** — the accepted, versioned creative direction for a project.

**Campaign** — a planned batch of compatible generation requests for one target.

**Candidate** — one attempt's output among several, compared during review.

**Super Shot** — one perceived shot of 2.0–30.0 seconds, generated directly or as an ordered chain of adjacent segments sharing boundary anchors and, once rendered, their exact images.
five-second segments and assembled into one playable video.

**Execution target** — somewhere generation can run (local, rented, API).

**Generation request** — a frozen specification revision plus production choices.

**Guided tour** — a scripted, software-driven walkthrough that operates the real
interface.

**Model profile** — named reusable production settings (checkpoint, etc.).

**Readiness** — a shot's derived production state.

**Segment** — one adjacent anchor-to-anchor generation inside a Super Shot; its start and end anchors are the specific boundary images for that interval, never silently the shot's outer endpoints.
video.

**Selection** — the current asset filling a shot role.

**Shot** — the smallest visual unit, beginning in one state and ending in
another.

**Specification** — the editable start-frame, end-frame or motion instructions.

**Workflow version** — an immutable imported ComfyUI API graph with bindings.

---

*vid-build 0.02 — idea to finished film, one visible change at a time.*
