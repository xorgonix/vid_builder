# vid-build 0.02 — Getting started

*Idea to finished film, one visible change at a time.*

This manual covers installation, first run, the core concepts and the
application shell. For the creative workflow see
[Creative development](02-creative-development.md); for generation and delivery
see [Production and delivery](03-production-and-delivery.md).

---

## Contents

1. [Introduction](#1-introduction)
2. [Prerequisites](#2-prerequisites)
3. [First run](#3-first-run)
4. [Core concepts and terminology](#4-core-concepts-and-terminology)
5. [The application shell](#5-the-application-shell)
6. [Accounts, workspaces and roles](#6-accounts-workspaces-and-roles)
7. [Where to go next](#7-where-to-go-next)

---

## 1. Introduction

vid-build is a **local-first video development studio**. It helps one creator
take a single idea through the whole craft of making a video:

> develop the idea → structure it → see it → block it → generate it → review it →
> assemble it → decide where to run the expensive parts.

It is built around three beliefs:

1. **You are the director, not a prompt operator.** The studio asks what a shot
   accomplishes, what the audience should notice, and what visibly changes.
2. **You should see the film before the pixels exist.** A storyboard of
   structured blocking sketches works with **no image model at all**, for free.
3. **AI proposes; humans apply and approve.** Generated briefs, outlines, shots,
   assessments and cost estimates never become authoritative on their own.

Everything runs on your machine. The database is a single SQLite file, your
media is a folder of files, and your models are whatever you already run in LM
Studio and ComfyUI.

---

## 2. Prerequisites

| Component | Needed for | Notes |
|---|---|---|
| **Go 1.26+** | building/running | `go version` |
| **templ** | regenerating templates | `go install github.com/a-h/templ/cmd/templ@latest` |
| **LM Studio** | the planner (brief/outline suggestions) | OpenAI-compatible server, default `http://127.0.0.1:1234` |
| **ComfyUI** | image/video generation | default `http://127.0.0.1:8188` |
| **FFmpeg** | export and duration-video assembly | must be on `PATH` |

Only Go is strictly required. Without LM Studio the planner features are
disabled but everything else works; without ComfyUI you can still develop,
outline, board and block shots; without FFmpeg you can do everything except
final export and duration-video assembly.

---

## 3. First run

```bash
# Git Bash
cd /d/Dev/vid_builder
cp config.example.json config.json
templ generate
go run ./cmd/vidbuild -open
```

```powershell
# PowerShell / cmd
cd D:\Dev\vid_builder
Copy-Item config.example.json config.json
templ generate
go run .\cmd\vidbuild -open
```

> **Path separators matter.** In Git Bash use `./cmd/vidbuild`; in PowerShell/cmd
> use `.\cmd\vidbuild`. A bare `go build` fails because the entrypoint lives in
> `cmd/vidbuild`.

The app writes defaults if `config.json` is missing, then opens
`http://127.0.0.1:8971`. Sign in with the bootstrap account defined in
`config.json`:

- default email: `admin@vidbuild.local`
- default password: `vidbuild`

On first run the app creates that administrator, a personal workspace, the
bundled workflow packages, and a local ComfyUI execution target.

> **New here?** Open **Guided tours** (`/tutorials`) and run *Create a workspace
> and project*. The software drives the real interface while a caption explains
> each step — see [Guided tours](05-guided-tours.md).

---

## 4. Core concepts and terminology

The model is deliberately simple. Learn these seven nouns and the app makes
sense.

| Term | Meaning |
|---|---|
| **Workspace** | An ownership and permission boundary — personal work or client work. |
| **Project** | A shared creative world: brief, references, characters, locations. |
| **Video** | One deliverable inside a project (e.g. "90s promo", "vertical cutdown"). |
| **Section → Scene → Beat → Shot** | The outline hierarchy. A **Beat** is a dramatic unit inside a scene; a **Shot** is the smallest unit that *begins in one state and ends in another*. |
| **Shot specification** | The editable instructions for a shot. Every shot has three: **start frame**, **end frame**, **motion**. |
| **Blocking** | A structured sketch of composition, poses, gaze, props and camera, rendered instantly as SVG. |
| **Generation request → attempt → asset** | A frozen request for work; immutable attempts (one per candidate); the assets produced. |

Supporting nouns:

| Term | Meaning |
|---|---|
| **Selection** | "This asset currently fills this shot's start/end/motion role." |
| **Assessment** | Advisory evidence (model or human): pass / review / fail. |
| **Approval** | Your accountable decision: approve / reject / approve-with-exception. |
| **Workflow** | A ComfyUI graph you authored and tested, imported as an immutable version. |
| **Binding** | A mapping from a vid-build concept (e.g. first-frame prompt) to a node input. |
| **Model profile** | Named production settings (checkpoint, etc.) you reuse. |
| **Execution target** | Somewhere work can run: local ComfyUI, a rented worker, an API. |
| **Campaign** | A planned batch of compatible work assigned to one target. |
| **Duration video** | A prompt plus a length (5, 15 or 30 seconds) compiled into fixed five-second segments and assembled into one playable video. |

**Shot readiness** moves through a ladder and is always visible:

```
incomplete → blocking_ready → concept_ready → endpoint_ready → video_ready → complete
```

---

## 5. The application shell

Every screen shares the same chrome:

- **Top bar** — the app name, the **workspace switcher**, breadcrumbs
  (Project / Video), and your account menu (Account, Guided tours, Administration
  for system admins, Sign out).
- **Left navigation** — Dashboard, Projects, then the primary creative path:
  **Develop · Outline · Board · Produce · Duration video · Review · Edit · More**,
  plus Settings, Local services and Guided tours.
- **Main area** — the current working surface.
- **Status strip** (footer) — app version and host notes.

Client workspaces carry a **`client` badge** in the switcher so you do not
confuse a client's work with your own.

---

## 6. Accounts, workspaces and roles

### 6.1 There is no public sign-up

Accounts are created by an administrator (this is deliberate — it suits studio
and client work).

- **Sign in** at `/login` with email and password.
- **Sign out** from the account menu (top-right). Signing out revokes that
  session.

### 6.2 Your account

**Account** (`/account`) shows your name, email, role and **active sessions**.
Each session row can be revoked — the fastest way to remove a lost laptop or an
old browser.

### 6.3 Workspaces

**All workspaces** (`/workspaces`):

- create a workspace with **New workspace** (name + kind: personal / client /
  team);
- open a workspace to reach its dashboard and projects.

The workspace switcher in the top bar jumps between workspaces without changing
who you are. Switching never carries an unauthorized URL across boundaries.

### 6.4 Members and roles

**Workspace settings → Manage members** (`/w/{workspace}/members`), owners only:

| Role | May do |
|---|---|
| **Owner** | Everything: membership, targets, deletion, LAN access. |
| **Editor** | Create/modify creative and production state; launch campaigns. |
| **Reviewer** | Comment, assess, approve/reject when assigned. |
| **Viewer** | Read permitted material and outputs. |

To add someone, they must already exist as a user. Enter their email, choose a
role, **Add member**. You can change a role/status inline, or remove the member.

**Guardrails:** you cannot demote, disable or remove the **last owner**, and you
cannot disable your own account. These actions are blocked with a clear message.

### 6.5 Administrating users

System administrators get **Administration** in the account menu
(`/admin/users`): create users (email, display name, password, optional
system-admin flag), and disable/enable accounts. Disabling a user immediately
invalidates their sessions on the next request.

---

## 7. Where to go next

- [Creative development](02-creative-development.md) — brief, outline, beats,
  blocking, continuity, animatic, prompt workbench.
- [Production and delivery](03-production-and-delivery.md) — workflows,
  generation, duration videos, review, edit, export, compute.
- [Operations and administration](04-operations-and-admin.md) — packages,
  backup, LAN, settings, configuration, troubleshooting.
- [Guided tours](05-guided-tours.md) — let the software show you around.

---

*vid-build 0.02 — idea to finished film, one visible change at a time.*
