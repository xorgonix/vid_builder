# vid-build 0.01

A local-first video development and production studio. A **Project** holds a shared
creative world; a **Video** is one ordered deliverable; Outline, Storyboard and Edit
are views over that one video. Every shot defines its **start frame**, **end frame**
and **motion** independently, and generation preserves lineage from creative decision
through workflow, model, attempt and selected asset.

Built from `SPECS/mark2-v3.1-specification.md`. The product name is **vid-build 0.01**.

## Stack

- **Go** (Chi router) with **templ** server-rendered components
- **HTMX 2.x** + **daisyUI v5** (CDN during development)
- **SQLite** (pure-Go `modernc.org/sqlite`) as the sole source of truth, WAL + STRICT tables
- **LM Studio** (planner) and **ComfyUI** (generation) reached over loopback
- **FFmpeg** for export
- Wails is a *host*, not the architecture: the same routes serve desktop and browser.
  Wails packaging/signing is explicitly deferred (spec §29).

## Getting started

Prerequisites: Go 1.26+, `templ`, FFmpeg on `PATH`. Optional: LM Studio on
`http://127.0.0.1:1234`, ComfyUI on `http://127.0.0.1:8188`.

```powershell
cp config.example.json config.json   # config.json is gitignored
templ generate
go run ./cmd/vidbuild -open
```

Open http://127.0.0.1:8971 and sign in with the bootstrap account from
`config.json` (default `admin@vidbuild.local` / `vidbuild`).

All runtime variables live in `config.json`, which is listed in `.gitignore`.
`config.example.json` is the committed template.

## The ComfyUI workflow

`internal/workflows/bundled/` contains a human-authored SDXL text-to-image package:

- `sdxl_txt2img_ui.json` — drag onto the ComfyUI canvas to install and edit.
- `sdxl_txt2img_api.json` — the API-format graph vid-build imports and binds.
- `manifest.json` — semantic bindings (which node inputs vid-build may supply).

vid-build never builds or edits node graphs in Go. It imports an immutable graph
snapshot, applies only human-confirmed bindings, and submits the concrete graph.

## How it works

1. **Develop** — record discovery, ask the LM Studio planner, and apply a brief
   proposal field-by-field. AI proposes; humans apply.
2. **Outline** — sections, scenes and shots; each shot is created with start, end
   and motion specifications.
3. **Board / Blocking** — structured blocking JSON rendered as deterministic SVG.
   The paired editor works with a JavaScript island and a no-JavaScript JSON fallback.
4. **Produce** — freeze a spec revision into a generation request (candidates, seed
   policy, quality tier, execution now or campaign later).
5. **Review** — compare candidates, select, assess (advisory) and approve/reject,
   including recorded exception approval.
6. **Edit / Export** — ordered clips, trims, cut/fade/dissolve transitions, FFmpeg
   assembly and a downloadable receipt.
7. **Compute** — estimate ready work across execution targets and plan render
   campaigns with preflight, launch, pause/cancel and reconciliation.

The durable worker claims jobs, runs adapters, imports verified outputs and records
measured observations. Attempts are immutable; rerendering creates new attempts.

## Layout

```
cmd/vidbuild          entrypoint (web/desktop launcher)
internal/config       runtime configuration (config.json)
internal/db           SQLite open + migrations
internal/store        repositories
internal/blocking     blocking schema + deterministic SVG
internal/workflows    bundled/imported workflow packages + binding application
internal/comfyui      ComfyUI HTTP client
internal/llm          LM Studio chat client
internal/planner      proposal prompts
internal/worker       durable job worker
internal/exporter     FFmpeg assembly
internal/media        atomic media storage + checksums
internal/desktop      narrow host bridge (domain/web never import Wails)
internal/web          Chi handlers, middleware, static assets
internal/templates    templ components
```

## Security

Argon2id passwords, opaque session tokens (only hashes stored), per-session CSRF
on every mutation, workspace-scoped authorization in the services, loopback-only
local services, and assets served through an authorized route. ComfyUI is private
infrastructure and is never exposed by the application listener.

## Status

Phases 0–7 of the specification are implemented. Deferred items are those listed
in spec §29 (Wails packaging/signing, rental/API adapters, audio, vector search).