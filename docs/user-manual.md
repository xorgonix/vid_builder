# vid-build 0.01 — User Manual

*Idea to finished film, one visible change at a time.*

This manual covers every screen and command in vid-build 0.01. It is written for the
person actually using the studio. A hands-on tutorial follows at the end.

---

## Contents

1. [Introduction](#1-introduction)
2. [Getting started](#2-getting-started)
3. [Core concepts and terminology](#3-core-concepts-and-terminology)
4. [The application shell](#4-the-application-shell)
5. [Accounts, workspaces and roles](#5-accounts-workspaces-and-roles)
6. [Project Bible](#6-project-bible)
7. [Develop — from ambiguity to a decision](#7-develop--from-ambiguity-to-a-decision)
8. [Outline — what the film says](#8-outline--what-the-film-says)
9. [Storyboard and blocking](#9-storyboard-and-blocking)
10. [Prompt workbench](#10-prompt-workbench)
11. [Shot inspector](#11-shot-inspector)
12. [Workflows — your ComfyUI graphs](#12-workflows--your-comfyui-graphs)
13. [Model profiles and execution targets](#13-model-profiles-and-execution-targets)
14. [Produce — generation](#14-produce--generation)
15. [Watching a job](#15-watching-a-job)
16. [Review — select and approve](#16-review--select-and-approve)
17. [Edit and export](#17-edit-and-export)
18. [Compute planner and campaigns](#18-compute-planner-and-campaigns)
19. [Project packages](#19-project-packages)
20. [Backup, verify and restore](#20-backup-verify-and-restore)
21. [LAN access — phone and tablet](#21-lan-access--phone-and-tablet)
22. [Settings and local services](#22-settings-and-local-services)
23. [Diagnostics reference](#23-diagnostics-reference)
24. [Security and privacy](#24-security-and-privacy)
25. [config.json reference](#25-configjson-reference)
26. [Command-line reference](#26-command-line-reference)
27. [Troubleshooting](#27-troubleshooting)
28. [Limitations and not-yet-implemented](#28-limitations-and-not-yet-implemented)
29. [Glossary](#29-glossary)
30. [Tutorial: your first film, end to end](#30-tutorial-your-first-film-end-to-end)

---

## 1. Introduction

vid-build is a **local-first video development studio**. It helps one creator take a
single idea through the whole craft of making a video:

> develop the idea → structure it → see it → block it → generate it → review it →
> assemble it → decide where to run the expensive parts.

It is built around three beliefs:

1. **You are the director, not a prompt operator.** The studio asks what a shot
   accomplishes, what the audience should notice, and what visibly changes.
2. **You should see the film before the pixels exist.** A storyboard of structured
   blocking sketches works with **no image model at all**, for free.
3. **AI proposes; humans apply and approve.** Generated briefs, outlines, shots,
   assessments and cost estimates never become authoritative on their own.

Everything runs on your machine. The database is a single SQLite file, your media is a
folder of files, and your models are whatever you already run in LM Studio and ComfyUI.

---

## 2. Getting started

### 2.1 Prerequisites

| Component | Needed for | Notes |
|---|---|---|
| **Go 1.26+** | building/running | `go version` |
| **templ** | regenerating templates | `go install github.com/a-h/templ/cmd/templ@latest` |
| **LM Studio** | the planner (brief/outline suggestions) | OpenAI-compatible server, default `http://127.0.0.1:1234` |
| **ComfyUI** | image/video generation | default `http://127.0.0.1:8188` |
| **FFmpeg** | export | must be on `PATH` |

Only Go is strictly required. Without LM Studio the planner features are disabled but
everything else works; without ComfyUI you can still develop, outline, board and block
shots; without FFmpeg you can do everything except final export.

### 2.2 First run

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

> **Path separators matter.** In Git Bash use `./cmd/vidbuild`; in PowerShell/cmd use
> `.\cmd\vidbuild`. A bare `go build` fails because the entrypoint lives in `cmd/vidbuild`.

The app writes defaults if `config.json` is missing, then opens
`http://127.0.0.1:8971`. Sign in with the bootstrap account defined in `config.json`:

- default email: `admin@vidbuild.local`
- default password: `vidbuild`

On first run the app creates that administrator, a personal workspace, the bundled
SDXL workflow package, and a local ComfyUI execution target.

---

## 3. Core concepts and terminology

The model is deliberately simple. Learn these seven nouns and the app makes sense.

| Term | Meaning |
|---|---|
| **Workspace** | An ownership and permission boundary — personal work or client work. |
| **Project** | A shared creative world: brief, references, characters, locations. |
| **Video** | One deliverable inside a project (e.g. "90s promo", "vertical cutdown"). |
| **Section → Scene → Shot** | The outline hierarchy. A **Shot** is the smallest unit that *begins in one state and ends in another*. |
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

**Shot readiness** moves through a ladder and is always visible:

```
incomplete → blocking_ready → concept_ready → endpoint_ready → video_ready → complete
```

---

## 4. The application shell

Every screen shares the same chrome:

- **Top bar** — the app name, the **workspace switcher**, breadcrumbs (Project / Video),
  and your account menu (Account, Administration for system admins, Sign out).
- **Left navigation** — Dashboard, Projects, then the primary creative path:
  **Develop · Outline · Board · Produce · Review · Edit · More**, plus Settings.
- **Main area** — the current working surface.
- **Status strip** (footer) — app version and host notes.

Client workspaces carry a **`client` badge** in the switcher so you do not confuse a
client's work with your own.

---

## 5. Accounts, workspaces and roles

### 5.1 There is no public sign-up

Accounts are created by an administrator (this is deliberate — it suits studio and
client work).

- **Sign in** at `/login` with email and password.
- **Sign out** from the account menu (top-right). Signing out revokes that session.

### 5.2 Your account

**Account** (`/account`) shows your name, email, role and **active sessions**. Each
session row can be revoked — the fastest way to remove a lost laptop or an old browser.

### 5.3 Workspaces

**All workspaces** (`/workspaces`):

- create a workspace with **New workspace** (name + kind: personal / client / team);
- open a workspace to reach its dashboard and projects.

The workspace switcher in the top bar jumps between workspaces without changing who
you are. Switching never carries an unauthorized URL across boundaries.

### 5.4 Members and roles

**Workspace settings → Manage members** (`/w/{workspace}/members`), owners only:

| Role | May do |
|---|---|
| **Owner** | Everything: membership, targets, deletion, LAN access. |
| **Editor** | Create/modify creative and production state; launch campaigns. |
| **Reviewer** | Comment, assess, approve/reject when assigned. |
| **Viewer** | Read permitted material and outputs. |

To add someone, they must already exist as a user. Enter their email, choose a role,
**Add member**. You can change a role/status inline, or remove the member.

**Guardrails:** you cannot demote, disable or remove the **last owner**, and you cannot
disable your own account. These actions are blocked with a clear message.

### 5.5 Administrating users

System administrators get **Administration** in the account menu (`/admin/users`):
create users (email, display name, password, optional system-admin flag), and
disable/enable accounts. Disabling a user immediately invalidates their sessions on the
next request.

---

## 6. Project Bible

**Project → Project Bible** (`/w/{workspace}/projects/{project}/bible`) holds the
reusable world of the project:

- **Characters** — names and descriptions/invariants (e.g. "Maya, ceramicist, clay-dusted apron").
- **Locations** — places and continuity notes.
- **Style rules** — project-wide visual rules ("warm film grain, shallow depth of field").

Style rules and the accepted brief's **visual style** become the *inherited base* for the
prompt workbench (see §10). Keep this page honest — it feeds every shot.

---

## 7. Develop — from ambiguity to a decision

**Develop** (`.../videos/{video}/develop`) is where an idea becomes accepted direction.

### 7.1 The two columns

- **Left** — the **Accepted brief** card and the **Proposals** list.
- **Right** — the **Discovery conversation**.

### 7.2 Recording the idea

Type into the composer ("A 20-second teaser for a handmade ceramic studio. Calm,
tactile, warm light.") and **Send**. Messages are append-only history: you never lose
what was said.

### 7.3 Asking the planner

**Ask planner** sends the conversation plus project context to the configured LM Studio
model and appends a planner reply. The planner **proposes and discusses**; it cannot
change your project. If LM Studio is unavailable the button is disabled and an error is
shown.

### 7.4 Proposing a brief

**Propose brief** asks the model for a **structured brief** (premise, theme, audience,
desired reaction, pace, excitement, music/sound intent, visual style, camera language,
production notes). It is stored as a *proposal* — nothing changes yet.

### 7.5 Applying a brief proposal

Open the proposal from the list. Each field appears with a **checkbox** and an editable
text box. Tick only the fields you accept (edit the wording freely), then **Apply
selected fields**. A new versioned brief is written and the older one is superseded.
Use **Reject** to discard with a record.

### 7.6 Revising the brief by hand

**Revise manually** opens a form for every brief field. Saving creates a new version —
the previous direction is never destroyed. **Brief history** shows all versions.

### 7.7 Proposing an outline

**Propose outline** produces a section → scene → shot structure. Apply it with **Apply
whole outline**; it creates the sections, scenes and shots (each shot with start, end
and motion specs). Reject if it does not read well.

> **Human authority:** proposals never silently rewrite your brief or outline. You
> choose the fields and you press Apply.

---

## 8. Outline — what the film says

**Outline** (`.../videos/{video}/outline`) shapes the narrative before visual detail.

- Each **section** groups scenes and carries a purpose and duration budget.
- Each **scene** carries a synopsis, purpose, pace and audience reaction.
- Each **beat** is a dramatic unit inside a scene: title, purpose, trigger, action,
  reaction, resulting state, intended audience effect and a duration budget.
- Each **shot** row shows title, purpose, duration and a **readiness badge**.

Actions: **Add section** (dialog), **Add scene** under a section, **Add beat** under a
scene, **Add shot** under a beat, **↑/↓** to reorder, **Edit** to change fields, **Delete**
to remove. Deleting a section rehomes its scenes rather than silently dropping them; a
beat that still owns shots, or a scene's only beat, cannot be deleted. **Open** a shot to
reach the inspector.

### 8.1 Implicit beats

Simple projects are unchanged: a scene starts with one **implicit beat** that holds its
shots and is hidden. The first time you add an explicit beat, the implicit beat is
promoted so the full hierarchy becomes visible. Nothing is lost and nothing is rewritten.

### 8.2 Proposing beats and shots separately

The planner is split so you decide at the right altitude:

- **Propose beats** (from a scene) asks for the scene's dramatic beats. The proposal page
  lets you untick beats, edit any field before applying, apply all, reject with an optional
  reason, or regenerate from explicit feedback.
- **Propose shots** (from a beat) asks for the *minimum* shots that communicate that beat —
  one shot is a valid answer. Each proposed shot carries its purpose, what the audience
  must notice, start/change/end states, framing, duration, transitions, continuity needs, a
  suggested generation method (`blocking_only`, `generated`, `stock`, `live_action`,
  `composite`, `still_with_motion`) and a justification. Applying a shot folds that
  guidance into the shot's treatment so nothing the planner explained is lost.

Proposal pages also show the **current structure** for comparison, and an open proposal is
marked **stale** (never deleted) when the scene, beat, video or brief it was based on has
changed since.

A shot's purpose should be a sentence a director would say out loud. If you cannot name
the visible change, the shot is not ready to produce.

---

## 9. Storyboard and blocking

### 9.1 The Board

**Board** (`.../videos/{video}/board`) is the visual centre of the app. Each card shows
**START · CHANGE · END** with readiness and duration.

- **Display modes:** *Compact* (small thumbnails), *Director* (large, default),
  *Production* (readiness emphasis).
- **Filters:** All, or by readiness (`incomplete`, `blocking_ready`, `concept_ready`,
  `endpoint_ready`, `video_ready`, `complete`).
- **Open** a shot or jump to its **Blocking** editor.

Blocking sketches render instantly as SVG — no model call, no cost.

### 9.2 The Blocking editor

**Blocking editor** (`.../shots/{shot}/blocking`) is a paired view: **start** and **end**
side by side with a **Detected change** panel between them.

Controls on each side:

- **Camera** — shot size (wide/medium/close/extreme close) and angle (eye/low/high/dutch).
- **Actors** — name, position (x,y), pose, facing, gaze target; **+ actor** / remove.
- **Props** — name, position; **+ prop** / remove.
- **Arrows** — movement or camera; **+ arrow** / remove.

Editing updates the SVG live. **Save start/end blocking** stores a new version.
**Duplicate from opposite endpoint** copies the other side as a starting point — useful
for continuity.

**Change analysis** compares start and end and reports shot-size, angle, camera
position, actor position/pose/gaze/turn, props and arrows. If nothing changes it tells
you plainly: *"No visible change detected — the shot lacks a payoff."*

Every control has a **JSON fallback** (expand "Edit as JSON"): the same data can be typed
directly, so blocking still works with JavaScript disabled.

### 9.3 Model-assisted blocking

**Suggest blocking** asks the planner for a structured start/end proposal, rendered as
two preview sketches. **Apply blocking** saves the proposals you tick as blocking
versions. Nothing is applied until you say so.

---

## 10. Prompt workbench

**Prompt workbench** (`.../shots/{shot}/prompt-workbench?kind=start_frame|end_frame|motion`)
edits creative instructions as **layers** — without exposing any node wiring.

### 10.1 Inherited context

The top card shows the **inherited base**: the accepted brief's visual style plus active
project style rules. This is automatically prepended during compilation.

### 10.2 Layers

Add layers with a **scope** and a **mode**:

| Scope | Use for |
|---|---|
| project_style | overall look |
| character | identity and invariants |
| location | place and continuity |
| first_frame / last_frame | endpoint states |
| motion | subject/camera/environment movement |
| workflow | model-specific guidance |
| experiment | temporary ideas |

| Mode | Effect on the compiled prompt |
|---|---|
| **inherit** | add as inherited context |
| **append** | append to the positive prompt |
| **replace** | replace the positive prompt entirely |
| **suppress** | push the text into the **negative** prompt |
| **lock** | protect the layer from AI overwrite (no text effect) |

### 10.3 Compile preview and save

**Compile preview** shows the exact positive and negative fields that will be bound —
*what the model will actually receive*. **Save as new specification version** writes a
new spec version carrying both the compiled prompts and the source layers.

First-frame and last-frame text are stored independently. If you write identical text
for both endpoints, expect a lint warning: the shot must express a visible change.

---

## 11. Shot inspector

**Shot inspector** (`.../shots/{shot}`) is the single place to examine everything about a
shot. Tabs:

1. **Intent** — title, purpose, treatment, duration.
2. **Start** — positive/negative prompts, structured spec, version.
3. **End** — same for the destination state.
4. **Motion** — subject, camera and environmental movement; negative motion.
5. **History** — specification versions.

Each tab offers:

- **Suggest with planner** — creates a proposal you review and apply (it never
  overwrites silently). Proposals of type `prompt_revision` appear in Develop.
- **Compare candidates** — jumps to the candidate comparison page for that role.
- **Prompt workbench** (header) — layered editing (§10).
- **Blocking editor** (header) — paired blocking (§9.2).

Saving a spec creates a **new version**; the previous version stays available to any
generation receipt that referenced it.

---

## 12. Workflows — your ComfyUI graphs

vid-build does **not** build node graphs in code. You author and test a graph in
ComfyUI, export it in **API format**, import it, and tell vid-build which inputs it may
supply. This is the most important screen for expert users.

### 12.1 Importing

**Workflows** (`/w/{workspace}/workflows`) → **Import a ComfyUI API-format workflow**:

1. In ComfyUI, build and successfully run your graph.
2. Export it in API format.
3. In vid-build, give it a **name**, choose a **task** (`image` or
   `first_last_video`), and either upload the JSON file or paste it.
4. **Import as immutable draft.**

The import computes a **SHA-256** of the graph, stores it immutably, inventories node
types, and **suggests bindings** (§12.2).

### 12.2 Bindings (what vid-build may change)

Open **Bindings** for the version. The table lists the known semantic fields with a
node id and an input name:

```
first_frame_positive   → node 2 . text
first_frame_negative   → node 3 . text
width / height         → node 4 . width / height
seed                   → node 5 . seed
model_name             → node 1 . ckpt_name
output_node            → node 7 (SaveImage / SaveWEBM)
```

Import suggestions map common types automatically:

- `CheckpointLoaderSimple` → `model_name` (input `ckpt_name`)
- `CLIPTextEncode` → `first_frame_positive` then `first_frame_negative` (input `text`)
- `LoadImage` → `first_frame_asset`, `last_frame_asset`, then `reference_asset` (input `image`)
- `EmptyLatentImage` → `width`, `height`
- `KSampler` / `KSamplerAdvanced` → `seed`
- `SaveImage` / `PreviewImage` → `output_node`

Tick a row to include it, set the node id and input name, and **Save bindings**. Only
confirmed bindings are ever written into a graph.

### 12.3 Validate and activate

**Validate against target** checks:

- every bound node and input still exists in the graph (missing = **fail**, never
  guessed);
- an `output_node` is bound;
- every node class exists in the running ComfyUI;
- every referenced checkpoint exists on the target.

Passing sets the version **validated**; failing sets **invalid** and lists the reasons.
**Activate** makes a validated version selectable in Produce. **Retire** stops new
requests while preserving historical receipts.

### 12.4 Endpoint-capable workflows

A workflow supports first/last video only if it declares both `first_frame_asset` and
`last_frame_asset` bindings. Produce refuses endpoint-conditioned motion on a workflow
that lacks them — there is **no silent start-only fallback** (§14.4).

---

## 13. Model profiles and execution targets

### 13.1 Model profiles

**Workspace settings → Model profiles** (`/w/{workspace}/model-profiles`): create named
profiles (name, task, checkpoint) chosen from checkpoints ComfyUI reports; delete ones
you no longer need.

- Set a **project default** on the project page (Project → Default model profile).
- The **generate form** lists profiles; picking one records it on the request, and the
  profile's checkpoint wins over the free-text checkpoint field.

### 13.2 Execution targets

**Compute → Execution targets** (`/w/{workspace}/execution-targets`): list the built-in
**Local ComfyUI** target and record additional targets (remote agent, hosted ComfyUI,
commercial API). Only metadata and pricing assumptions live here — never raw secrets.
Targets marked with measured observations show their average.

---

## 14. Produce — generation

**Produce** (`.../videos/{video}/produce`) turns ready work into controlled requests.

### 14.1 Ready work table

Each shot row shows readiness and which roles are ready (start / end / motion), with a
**Start / End / Motion** button that opens the generation form for that role.

### 14.2 The generation form

| Field | Meaning |
|---|---|
| **Workflow version** | a validated workflow (bundled or imported) |
| **Model profile** | optional; its checkpoint takes precedence |
| **Positive / negative** | prefilled from the current spec; editable for this request |
| **Width / Height** | defaults from the video's format |
| **Candidates** | how many attempts to produce (1–8) |
| **Quality tier** | concept or final |
| **Seed policy** | random · stable variants · explicit |
| **Seed** | base seed (for stable/explicit) |
| **Checkpoint** | chosen from ComfyUI's installed list |
| **Execution** | *Local now* (run immediately) or *Campaign later* (plan it) |

The form shows live ComfyUI reachability; if ComfyUI is unreachable the **Queue**
button is disabled and the reason is shown.

### 14.3 What happens on submit

Submitting **freezes** the current specification revision into a **generation request**
and creates one immutable **attempt per candidate**, then a durable **job**. You are
taken to the job page.

If you chose *Campaign later*, no job is queued; the request waits under **Compute**.

### 14.4 Endpoint-conditioned motion (important)

For a **motion** request, the form offers **"Require first + last frames"**. When
checked, the request is refused unless:

1. both endpoints are **selected**;
2. both are **approved** (or **exception-approved**);
3. the chosen workflow declares `first_frame_asset` and `last_frame_asset` bindings.

When it runs, the worker stages both frames to ComfyUI and records **both checksums** in
the receipt. The job page's **Endpoints** column proves it. vid-build will not quietly
degrade a two-endpoint shot to a start-only render — a start-only clip is a different,
deliberate request (uncheck the box).

---

## 15. Watching a job

**Job page** (`.../jobs/{job}`) auto-refreshes and shows:

- **State** — queued → claimed → running → succeeded / failed.
- **Events** — a live timeline: validate, stage inputs, load models, submit, generate,
  collect, verify, persist, cleanup, plus **memory coordination** lines.
- **Attempts** — one per candidate with status, seed, model, duration, **endpoints**
  (for motion) and any error.
- **Outputs** — the produced assets with thumbnails.

Every attempt is immutable. **Retry** produces a *new* attempt rather than overwriting
the failed one.

---

## 16. Review — select and approve

**Review** (`.../videos/{video}/review`) is your inbox. Candidates are grouped by shot
and role.

### 16.1 Choosing

On each candidate card:

- **Select** — makes this the current asset for that shot role.
- **Approve** — your accountable decision.
- **Reject** / **Exception approve** — require a **reason**.
- **Assess** — record a human assessment (pass / review / fail). Assessments are
  **advisory**; they never approve or reject for you.

Cards show *selected*, the current decision, and a **stale** badge when the shot changed
since the selection was made.

### 16.2 Candidate comparison

**Compare** (or **Compare candidates** on a shot) opens
`.../shots/{shot}/candidates/{role}`: full-size candidates in a grid, each with its
**receipt** — model, seed, workflow SHA, duration, checksum — and the same
select/approve/exception/assess actions. Judge at full size, not from a thumbnail.

### 16.3 Annotations

The **Annotations** card collects review notes. Add an observation (optionally attached
to a shot) and **Resolve** it when addressed. Write actionable notes ("continuity: apron
colour differs from shot 2"), not vague dislike.

> **The rule:** selection is not approval. A model assessment is not approval. Approval
> is a human decision, and it can be an **exception approval** with a recorded reason.

---

## 17. Edit and export

**Edit** (`.../videos/{video}/edit`) assembles your call sheet into one deliverable.

### 17.1 Building the cut

- **Selected assets** (right column) lists every current selection; **Add** places it on
  the timeline.
- Each clip shows its asset, shot, duration and transitions.
- **Duration (s)** trims a clip; **In / Out** set **cut / fade / dissolve**; **Save**
  applies.
- **↑ / ↓** reorder; **Remove** takes a clip out of *this cut* — the asset is retained.

### 17.2 Validate

**Validate** checks for missing assets and missing checksums and reports warnings you can
resolve or accept.

### 17.3 Export

**Export** opens a profile summary (the video's resolution and frame rate) and **Queue
export**. The worker assembles a single **MP4** (H.264, `yuv420p`, faststart) using
FFmpeg.

- **Cut** transitions are rendered as a near-instant transition.
- **Fade** and **Dissolve** are composited with a crossfade so **every clip is
  preserved**.

The **export receipt** (`/exports/{id}`) plays the result, offers a **download**, and
shows the checksum — linking the finished film back to its source revision and assets.

---

## 18. Compute planner and campaigns

**Compute** (`.../videos/{video}/compute`) answers: *where, when and at what
approximate cost should ready work run?*

### 18.1 Planning

- **Requests** shows work saved for planning ("campaign later").
- **Target comparison** shows each target with its **observed average** per candidate.
  Until you have measured runs it says *"assumed (no observations yet)"* and uses a
  default. Estimates are built from **your** measurements.

### 18.2 Campaigns

Create a **draft campaign** naming a target; it collects the waiting requests.
On the **campaign page**:

1. **Preflight** — verifies the target (ComfyUI reachable for local), that every item
   still has a workflow, and that nothing is missing. Passing marks it **ready**.
2. **Launch** — queues the jobs. A campaign will not launch in an invalid state.
3. **Pause** — stops claiming new items; running work continues.
4. **Cancel** — requests cancellation; produced assets and charges are preserved.
5. **Reconcile actuals** — records actual time/cost so future estimates improve without
   rewriting history.

Two GPUs are modelled as **independent workers**; VRAM is never summed.

---

## 19. Project packages

**Project → Package** (`/w/{workspace}/projects/{project}/package`) previews a portable
export: counts of videos, sections, scenes, shots, bible entries, briefs, proposals and
media files. **Queue portable package** builds a ZIP containing:

- `project.json` — schema version and all project rows;
- `manifest.json` — the media list with checksums;
- `media/…` — the referenced files.

Download the latest package from the same page.

---

## 20. Backup, verify and restore

### 20.1 Creating a backup

**Workspace → Diagnostics → Backups → Queue backup** (owners). A backup is:

- a **consistent database copy** (SQLite `VACUUM INTO`), and
- a **media manifest** listing every media URI with its SHA-256.

A database-only copy is never labelled complete.

### 20.2 Verifying

**Verify** re-checks the database integrity and **re-hashes every referenced media
file**. The result reports e.g. *"database integrity ok, 12/12 media checksums match"*,
or names the first file that is missing or changed.

### 20.3 Restoring (offline)

Restore is a command-line tool — stop the application first:

```bash
# dry run: verify the backup and report, change nothing
go run ./cmd/vidbuild-restore -backup data/backups/backup-YYYYMMDD-HHMMSS

# perform the swap: keeps a safety copy of the current database, then replaces it
go run ./cmd/vidbuild-restore -backup data/backups/backup-YYYYMMDD-HHMMSS -yes
```

Restore verifies the backup, copies the current database aside (`*.pre-restore-*`),
atomically swaps in the backup, and confirms integrity. Media files stay in the media
store and are verified against the manifest.

---

## 21. LAN access — phone and tablet

**Workspace → Settings → LAN access** (owners). LAN is **off by default**.

1. **Enable LAN access** — starts a separate listener (`lan_bind_host`:`lan_port`) and
   issues a **pairing code** valid for ten minutes, **single use**.
2. On the phone, open the shown address and enter the code (or open the pairing link).
   The device receives its **own session**, separate from your desktop.
3. Manage or revoke devices under **Account → Active sessions**.
4. **Disable and revoke** closes the listener and invalidates outstanding codes.

The device talks only to vid-build. It never receives ComfyUI credentials or filesystem
paths, and local ComfyUI is never exposed.

---

## 22. Settings and local services

**Local services** (`/settings/local-services`):

- **Probe all services** — checks LM Studio (and names the model), ComfyUI (version and
  VRAM) and FFmpeg (on `PATH`), and reports the results.
- **Memory coordination** — see the configured unload command and **Unload planner now**.
  Before local GPU work the worker runs the unload command to free the planner's VRAM,
  records the action (or failure) as a job event, and reloads after work if configured.

**Workspace settings** (`/w/{workspace}/settings`) gathers identity, **Membership**,
**Execution targets**, **Model profiles** and **LAN access**.

---

## 23. Diagnostics reference

**Workspace → Diagnostics** (`/w/{workspace}/diagnostics`) reports:

| Reading | Meaning |
|---|---|
| Jobs queued / running / failed | worker health |
| SQLite integrity | `ok` or the failure |
| Foreign-key violations | should be 0 |
| Journal mode | `wal` |
| Media root / assets / attempts | storage overview |
| Working directory / platform | environment |
| Backups + Verify | §20 |
| **Generate missing thumbnails** | derive previews for existing image assets |

---

## 24. Security and privacy

- **Passwords** are stored as Argon2id hashes.
- **Sessions** are opaque tokens; only a SHA-256 hash is stored. Every mutation
  requires a **CSRF** token.
- **Authorization** is enforced in the services and is **workspace-scoped** — a project,
  asset or job never crosses a workspace boundary without permission.
- **Local services** are reached over loopback only; ComfyUI is never reverse-proxied.
- **LAN access** is opt-in, paired, per-device revocable, and disabled by default.
- **Secrets** never enter the database, HTML, job payloads or generation receipts.
- Every consequential action records the **human actor** in the audit log.

---

## 25. config.json reference

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
- `llm.unload_before_gpu` + `llm.unload_command` implement memory coordination (§22).
- `security.cookie_secure` should be `true` behind HTTPS; keep `false` for plain local
  HTTP.
- `server.mode` is informational metadata (`desktop` / `local-web` / `lan` / `server`);
  LAN availability is controlled at runtime from the UI.

---

## 26. Command-line reference

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

## 27. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `no Go files in D:\Dev\vid_builder` | You ran `go build` at the root. Use `go build ./cmd/vidbuild`. |
| `no required module provides package .cmdvidbuild` | Git Bash ate the backslashes. Use `./cmd/vidbuild`. |
| Sign-in fails | Check the `bootstrap` credentials in `config.json`; the account exists only if a user was created. |
| Planner buttons disabled / "Planner unavailable" | LM Studio not reachable or `llm.enabled=false`. Check Local services → Probe. |
| "No validated workflow available" | Import a workflow and **Validate** it (or use the bundled SDXL package). |
| Generate button disabled | ComfyUI unreachable. Start it and reload the form. |
| "ComfyUI unreachable" during a job | The worker reports the preflight failure; the request is marked failed. |
| Motion refused | Select **and approve** both endpoints, and choose a workflow that declares first/last bindings. |
| "Cannot demote/remove the last owner" | Add another owner before changing yours. |
| 403 on submit | Session expired or CSRF mismatch — reload and sign in again. |
| "Section/Scene/Shot changed elsewhere" | Optimistic revision conflict; the page reloaded with the current data. |
| Export fails | FFmpeg missing from `PATH`, or a clip's asset is missing. Run Diagnostics. |
| Video workflows fail with `hostbuf_file_reader_read failed` | A ComfyUI-side weight-loading bug (its `comfy_aimdo` offload). Update ComfyUI or disable its dynamic-VRAM offload. |
| Data looks empty | You may be in the wrong workspace (see the switcher) or signed in as a different user. |

---

## 28. Limitations and not-yet-implemented

vid-build 0.01 is a working vertical slice. Known gaps:

- **Wails packaging and signing** — the app runs as a local web app; the Wails desktop
  host is scaffolded conceptually but not packaged.
- **Rental/commercial API adapters** — targets can be recorded and campaigns planned,
  but only the local ComfyUI target executes today.
- **Global keyboard shortcuts** (e.g. `g d`, `/` search) are specified but not yet bound.
- **Timeline drag** — ordering uses Move Up/Down and numeric trims; scrubbing is not yet
  a drag surface.
- **Model-based asset assessment** — human assessment works; automated model assessment
  is not wired to a vision model.
- **Audio** — audio intent is modelled on the brief; there is no audio generation yet.
- **Vector search** — not implemented.
- **Restore UI** — restore is a CLI tool by design (safer than a live in-app swap).

---

## 29. Glossary

**Asset** — a produced file (image, video, document, export, thumbnail), stored on disk
with a checksum.

**Attempt** — one immutable execution of a generation request for one candidate.

**Binding** — a confirmed mapping from a semantic field to a ComfyUI node input.

**Blocking** — structured JSON describing composition/pose/camera, rendered as SVG.

**Brief** — the accepted, versioned creative direction for a project.

**Campaign** — a planned batch of compatible generation requests for one target.

**Candidate** — one attempt's output among several, compared during review.

**Execution target** — somewhere generation can run (local, rented, API).

**Generation request** — a frozen specification revision plus production choices.

**Model profile** — named reusable production settings (checkpoint, etc.).

**Readiness** — a shot's derived production state.

**Selection** — the current asset filling a shot role.

**Shot** — the smallest visual unit, beginning in one state and ending in another.

**Specification** — the editable start-frame, end-frame or motion instructions.

**Workflow version** — an immutable imported ComfyUI API graph with bindings.

---

## 30. Tutorial: your first film, end to end

This walkthrough takes about thirty minutes and produces a two-shot film with approved
concept frames, an assembled cut and an exported MP4. Where a step needs a service, the
optional shortcut is noted.

### Before you begin

Start LM Studio (with your model loaded) and ComfyUI. Verify:

- **Local services → Probe all services** should report LM Studio, ComfyUI and FFmpeg
  all `ok`. If ComfyUI is down, you can still complete Parts 1–3.

### Part 1 — Set up (5 minutes)

1. Run the app and **sign in** as the administrator.
2. Open **All workspaces → New workspace**, name it *Tutorial Studio*, kind *Personal*,
   and **Create**.
3. Click **New project** and fill in:
   - Project title: `Ceramic Studio Teaser`
   - First video title: `20s Teaser`
   - Purpose: `show the tactile craft of a potter's studio in twenty seconds`
   - Format: `Landscape`, Target duration: `20` seconds.
   **Create project.**

You land on the video page. Notice **Brief incomplete** — the app is telling you the
truth.

4. Open **Project Bible** and add:
   - Character: `Maya` — `ceramicist, clay-dusted apron, hair tied back`
   - Location: `Studio` — `warm window light, wooden workbench, spinning wheel`
   - Style rule — `warm film grain, shallow depth of field, natural light`

### Part 2 — Develop the brief (5 minutes)

5. Open **Develop**. In the composer write:
   > *A twenty-second teaser for a handmade ceramic studio. Calm, tactile, warm light.
   > We should feel the care in the craft.*
   Press **Send**.
6. Press **Ask planner** and read the reply. (If LM Studio is off, skip to step 8.)
7. Press **Propose brief**, then open the proposal from **Proposals**. Untick anything
   you disagree with, edit the `visual_style` field to
   `warm film grain, shallow depth of field, natural window light`, and press
   **Apply selected fields**. The **Accepted brief** card now shows a version badge.

*Checkpoint:* Develop shows an accepted brief; the video page no longer says "Brief
incomplete".

### Part 3 — Outline and blocking (8 minutes)

8. Open **Outline** and add a section `Craft`, with purpose `establish the material`.
9. Add two scenes under it: `Opening` (purpose `set the scene`) and `Hands on clay`
   (purpose `show the making`).
10. Under `Opening`, **Add shot**:
    - Title: `Studio wide`
    - Purpose: `introduce the warm studio and Maya at the wheel`
    - Duration: `4` seconds.
    Under `Hands on clay`, add a second shot `Hands` (purpose `reveal texture and care`).
11. Open the first shot (**Open**) → **Blocking editor**.
    - Set camera **shot size** to *wide*.
    - Add/adjust the actor so Maya sits left of centre facing the wheel.
    - Save the **start** blocking.
    - Press **Duplicate from opposite endpoint** on the **end** side.
    - Change the end: move the actor, change **pose** to `hands working clay`, and save.
    - Read **Detected change** — it should mention position and pose.
12. Return to **Board**. Your card now shows START and END sketches and a readiness badge.

*Checkpoint:* you can *see* the shot for free, with no model calls.

### Part 4 — Generate concept frames (7 minutes)

13. Open the first shot → **Start** tab → **Suggest with planner** (or write a prompt
    yourself): `potter at the wheel, wide studio shot, warm window light, clay dust`.
    Apply it if you used a suggestion.
14. Go to **Produce**. In the `Studio wide` row press **Start**.
15. On the form, set **Candidates** `2`, **Quality tier** `concept`, **Seed policy**
    `stable variants`, choose your checkpoint, and leave **Execution** on *Local now*.
    Press **Queue generation request**.
16. On the **job page**, watch the events move through the phases. When it reaches
    **succeeded**, two outputs appear.

*Checkpoint:* the job's **Attempts** table shows two rows, each with its own seed.

### Part 5 — Review and approve (3 minutes)

17. Open **Review**. Under `Studio wide · Start frame` you have two candidates.
18. Press **Compare** to see both at full size with their receipts, then **Select** the
    one you prefer, and **Approve** it.
19. Add an **Annotation**: `keep this warmth for the hands shot` and leave it open.

*Checkpoint:* the review card shows *selected* and *approve*.

### Part 6 — Repeat for the second shot

20. Repeat Parts 3–5 for the `Hands` shot: block it, generate two concept frames, select
    and approve the best.

### Part 7 — Assemble and export (5 minutes)

21. Open **Edit**. In **Selected assets**, press **Add** for the `Studio wide` selection
    and again for `Hands`.
22. Set the first clip's **Duration** to `4`, the second to `3`. Set the first clip's
    **Out** transition to `dissolve` and **Save**.
23. Press **Validate** — expect *"all clips present"*.
24. Press **Export**, then **Queue export**. When the receipt shows **complete**, play
    the video inline and **Download** the MP4.

*Checkpoint:* you have a real multi-shot MP4 on disk, with a receipt.

### Part 8 — Plan compute (optional, 3 minutes)

25. Back in **Produce**, queue a request with **Execution** set to *Campaign later*.
26. Open **Compute**: the request appears as a planned item, and the target table shows
    an estimate (marked *assumed* until you have more measured runs).
27. **Create draft campaign**, open it, **Preflight**, then **Launch**; watch the job
    run, then **Reconcile actuals** so the estimate improves next time.

### Part 9 — Protect and share (optional, 3 minutes)

28. **Diagnostics → Queue backup**, then **Verify** it — expect *"integrity ok, N/N media
    checksums match"*.
29. **Settings → LAN access → Enable**, note the code, and on a phone open the address
    and enter the code. Approve a candidate from the couch, then **Disable and revoke**.
    Revoke the device later from **Account → Active sessions** if you wish.

### Part 10 — Bring your own workflow (optional, advanced)

30. In ComfyUI, build and run a graph (e.g. your own SDXL or first/last video pipeline),
    export it in **API format**.
31. In vid-build: **Workflows → Import** (name, task, upload/paste), open **Bindings**,
    confirm the mapped node inputs, **Validate against target**, then **Activate**.
32. Return to a shot's generate form — your workflow now appears in the **Workflow
    version** list.

### You are done

You have taken one idea from a sentence to an approved, exported film — and every
decision, attempt and asset along the way is recorded and reproducible.

---

*vid-build 0.01 — idea to finished film, one visible change at a time.*
