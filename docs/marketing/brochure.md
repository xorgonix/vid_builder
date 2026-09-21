# vid-build 0.01 — Product Brochure

> **Direct the film. Don't babysit the prompt.**
> A local-first studio that turns one idea into a finished, multi-shot video — and keeps
> every human decision on the record.

*Idea to finished film, one visible change at a time.*

---

## The problem

AI video tools got powerful. The work around them did not.

- **The idea lives everywhere and nowhere.** A note here, a doc there, a dozen prompt
  tabs, and a folder of numbered PNGs.
- **You pay for pixels before you know the film works.** Renders are the most expensive
  way to discover a story problem.
- **Every result is a slot machine.** You get an output but not the recipe — so you
  can't reproduce last week's good take.
- **Decisions evaporate.** *Which model? which seed? why did I keep this one?* Nobody
  knows, least of all next month.
- **Cost is a surprise.** Local GPU, rented GPU or API? Most tools don't help you decide,
  and none reconcile what you actually spent.
- **Cloud convenience costs control.** Your creative work, client material and spend
  leave your machine.

If you make video seriously, you already have a production problem, not a prompt problem.

---

## The shift

**Making video is production, not prompting.**

vid-build treats video the way a studio does: with a brief, an outline, a storyboard,
blocking, deliberate production, review, and an edit — and with a human approving every
decision that matters. The software handles the tedious parts and shows its work.

Three promises:

1. **You are the director.** The studio asks what a shot accomplishes and what visibly
   changes. Model names and samplers are production settings, not direction.
2. **See the film before the pixels exist.** A storyboard of structured *blocking
   sketches* works with no image model at all — for free, instantly.
3. **AI proposes; you apply and approve.** Generated briefs, outlines, shots,
   assessments and cost estimates never become authoritative on their own.

---

## How it works

```
Develop  →  Outline  →  Board  →  Produce  →  Review  →  Edit  →  Compute
```

The order is increasing commitment. You may move freely; the studio always shows what is
actually ready rather than pretending later stages are complete.

| Stage | What happens | What you decide |
|---|---|---|
| **Develop** | Discuss the idea; the planner proposes a structured brief | Which fields to accept |
| **Outline** | Sections, scenes and shots, each with a purpose | Narrative order and emphasis |
| **Board** | Start · Change · End storyboard cards with blocking sketches | Staging, continuity, visible change |
| **Produce** | Freeze a spec into a generation request | Candidates, quality, where it runs |
| **Review** | Compare candidates and receipts | Select and approve (or reject) |
| **Edit** | Order, trim, transitions, export | Rhythm and sequence |
| **Compute** | Estimate and plan where expensive work runs | Target, spend, timing |

---

## Feature highlights

### Develop — from ambiguity to a decision
The Discover screen holds a **conversation** and an **accepted brief**. Ask the built-in
planner to reflect and refine; request a **brief proposal**; then tick only the fields you
agree with. Conversation is evidence. The accepted brief is authority. Direction is
versioned: revising it never destroys the previous version.

### Outline — decide what the film says
Sections, scenes and shots with purposes, reactions and duration budgets. Every shot is
created with **three specifications**: start frame, end frame and motion. The total
duration stays honest before anything renders.

### Board — see the film before the pixels
Every shot card shows **START · CHANGE · END**. The end state must visibly pay off the
start; the studio warns you when it doesn't. Filter by readiness and switch between
Compact, Director and Production views. **No model call, no cost.**

### Blocking — stage the shot cheaply
A **paired blocking editor** for both endpoints: camera, subjects, pose, gaze, props and
movement arrows, rendered instantly as clean SVG. A change-analysis panel reports pose,
position, gaze, prop and camera differences — and tells you plainly when nothing changes.
Blocking is structured data, so it survives reload and can be regenerated; a JSON view
means it works even with JavaScript off.

### Prompt workbench — layers, not spaghetti
Compose prompts as **layers** (project style, character, location, endpoints, motion,
experiments), each **inherit / append / replace / suppress / lock**. See the **exact
compiled prompt** before anything runs. First-frame and last-frame text are always kept
separate, with a lint warning if they fail to express a change.

### Produce — spend deliberately
Freeze the current specification into a **generation request**: candidates, quality tier,
seed policy, model profile, and **local now or campaign later**. Blocking and concepts are
cheap; final renders are a decision, not a default.

### Review — choose with evidence
Compare full-size candidates with a **receipt** for each: model, seed, workflow hash,
duration, checksum. **Select** the take you want, then **approve**, reject, or **approve
with a recorded exception**. Automated assessments are advice; the approval is yours.

### Edit & export — assemble the cut
Order approved clips, trim them, choose cut / fade / dissolve, validate, then export a
single **MP4** with a receipt that links the finished film back to its source assets.

### Compute planner — where should this run?
Estimates built from **your measured runs**, not vendor benchmarks. Compare local GPU,
rented workers and APIs. Bundle ready work into a **campaign**, preflight it, launch it,
then **reconcile actual cost**. Two GPUs are modelled as independent workers — VRAM is
never summed.

### The project Bible — a consistent world
Characters, locations and style rules live with the project and feed every shot. Change
a continuity-bearing entity and the studio shows you what it affects.

### Built for the couch, too
Pair a phone or tablet over **LAN** with a short-lived, single-use code to review,
comment and approve. Each device gets its own revocable session; your local models stay
private.

---

## Who it's for

**Solo AI-video creators.** You have the models and the GPU, but the craft is drowning in
tabs and guesswork. vid-build gives you a spine: idea → shot list → storyboard →
approved frames → cut.

**Small studios.** You need repeatability and a paper trail. Every attempt, decision and
cost is recorded and attributable — and you can hand a client a packaged project.

**Privacy-sensitive teams.** Client material, unreleased campaigns, regulated content.
Everything runs on your hardware, over your models; ComfyUI is never exposed.

---

## Why it's different

| Everyone else | vid-build |
|---|---|
| Prompt box, one take | Storyboard-first; see start · change · end before rendering |
| Black-box model | You import and bind **your own** tested ComfyUI workflows |
| Selection = whatever generated | Explicit select + human approval, with recorded exceptions |
| Cost is a surprise | Measured estimates, campaigns, preflight and reconciliation |
| Cloud lock-in | Local-first, SQLite source of truth, portable project packages |
| Node-graph lock-in | Semantic bindings make any workflow portable across models |

---

## Local-first by design

- **Your machine.** The studio runs on your workstation; nothing is uploaded to us.
- **One database.** A single SQLite file is the source of truth; media is a folder of
  files with checksums.
- **Your models.** Talk to LM Studio and ComfyUI over loopback. Author workflows in
  ComfyUI; vid-build imports, binds and executes them — it never rebuilds your graph.
- **Portable.** Export a project package (schema, rows, media manifest, files) at any time.
- **Recoverable.** Coordinated backups (consistent database copy + media manifest), a
  verify step that re-hashes every file, and an offline restore command.
- **Optional server mode.** The same application can run as a real web app for a team,
  with workspaces, roles and per-device sessions.

---

## Under the hood

For the technically curious (and reassuring for the cautious):

- **Go** with **Chi**, server-rendered **templ** components and **HTMX** — the interface
  is real HTML, not a client-side state machine.
- **SQLite** in WAL mode with `STRICT` tables, optimistic revisions and partial unique
  indexes that enforce "exactly one current" brief, spec, selection and blocking.
- A **durable job worker** with leases, phases and event history; generation attempts are
  **immutable**, so retrying never overwrites evidence.
- **Workflow ownership is explicit:** ComfyUI owns the graph; vid-build owns intent,
  prompt layers, bindings, submission and receipts. A missing node fails validation — it
  is never guessed.
- **Endpoint-conditioned video:** a motion request that requires first and last frames
  cannot start without both approved endpoints and a workflow that binds them. There is
  no silent start-only fallback.
- **Security:** Argon2id passwords, hashed session tokens, CSRF on every mutation,
  workspace-scoped authorization, loopback-only local services.

---

## Honest status

This is **vid-build 0.01**, a working vertical slice — not a mockup.

- The full path works today: create a project → develop a brief → outline → block →
  generate on a live ComfyUI → review and approve → edit → export an MP4.
- Verified against a real local stack (LM Studio planner, ComfyUI on an RTX 3090,
  FFmpeg).
- Still maturing: desktop packaging, rented/API execution adapters, audio, and some
  convenience features. See *Roadmap*.

We would rather tell you what works than what sounds impressive.

---

## Roadmap

| Horizon | Focus |
|---|---|
| Near | Desktop packaging and signing; onboarding |
| Mid | Rented-worker and commercial-API adapters; multi-user review; workflow library |
| Long | Hosted team coordinator; shared campaigns; production analytics |

---

## System requirements

- **Go 1.26+** to build and run
- **LM Studio** (optional) for the planner
- **ComfyUI** (optional) for image/video generation
- **FFmpeg** (optional) for export
- A modern browser (the desktop host is coming)

---

## Get started

```bash
git clone <repo>
cd vid_builder
cp config.example.json config.json
templ generate
go run ./cmd/vidbuild -open
```

Then: create a workspace, create a project, and follow the tutorial in the user manual.
You can storyboard a whole film before spending a single GPU-second.

---

## Frequently asked

**Do I need ComfyUI?**
No. You can develop, outline, storyboard and block without it. It is needed only for
generated frames.

**Do I need to be online?**
No. It is local-first; CDN assets are a development convenience, and release assets are
vendored.

**Will it replace my ComfyUI workflows?**
No — the opposite. You author and test graphs in ComfyUI; vid-build imports them as
immutable versions and binds only the inputs it is allowed to change.

**Can it make the film for me automatically?**
Deliberately not. It is built so a human decides, selects and approves. That is the
feature.

**Is my client material safe?**
It stays on your machine. Local ComfyUI is reached over loopback and never exposed;
LAN review pairs devices to vid-build, not to your model server.

---

## Messaging notes (internal)

*Headline alternatives tested for the landing page:*
- **A.** Direct the film. Don't babysit the prompt. *(recommended — names the audience
  shift and the pain in one line)*
- **B.** Storyboard the film before you spend a GPU-second. *(benefit-forward, concrete)*
- **C.** The studio for AI video that keeps you in charge. *(broadest, least specific)*

*Primary CTA alternatives:* "Run the studio locally" (recommended, honest for a
local-first tool) · "See your first storyboard" (outcome) · "Get vid-build" (blunt).

*Voice:* confident, concrete, unhyped. Benefits over features. No invented metrics, no
fabricated testimonials — the product's stated advantage is discipline, and the copy
should model it.
