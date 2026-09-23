# vid-build 0.02 — Production and delivery

*From specifications to approved, assembled, exported work.*

This manual covers **Workflows**, **model profiles and execution targets**,
**Produce**, **Super Shot**, **watching a job**, **Review**, **Edit and
export**, and the **compute planner and campaigns**.

---

## Contents

1. [Workflows — your ComfyUI graphs](#1-workflows--your-comfyui-graphs)
2. [Model profiles and execution targets](#2-model-profiles-and-execution-targets)
3. [Produce — generation](#3-produce--generation)
4. [Super Shot — 2.0 to 30.0 seconds](#4-super-shot--20-to-300-seconds)
5. [Watching a job](#5-watching-a-job)
6. [Review — select and approve](#6-review--select-and-approve)
7. [Edit and export](#7-edit-and-export)
8. [Compute planner and campaigns](#8-compute-planner-and-campaigns)

---

## 1. Workflows — your ComfyUI graphs

vid-build does **not** build node graphs in code. You author and test a graph in
ComfyUI, export it in **API format**, import it, and tell vid-build which inputs
it may supply. This is the most important screen for expert users.

### 1.1 Importing

**Workflows** (`/w/{workspace}/workflows`) → **Import a ComfyUI API-format
workflow**:

1. In ComfyUI, build and successfully run your graph.
2. Export it in API format.
3. In vid-build, give it a **name**, choose a **task** (`image` or
   `first_last_video`), and either upload the JSON file or paste it.
4. **Import as immutable draft.**

The import computes a **SHA-256** of the graph, stores it immutably, inventories
node types, and **suggests bindings** (§1.2).

### 1.2 Bindings (what vid-build may change)

Open **Bindings** for the version. The table lists the known semantic fields with
a node id and an input name:

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
- `CLIPTextEncode` → `first_frame_positive` then `first_frame_negative`
  (input `text`)
- `LoadImage` → `first_frame_asset`, `last_frame_asset`, then `reference_asset`
  (input `image`)
- `EmptyLatentImage` → `width`, `height`
- `KSampler` / `KSamplerAdvanced` → `seed`
- `SaveImage` / `PreviewImage` → `output_node`

Tick a row to include it, set the node id and input name, and **Save bindings**.
Only confirmed bindings are ever written into a graph.

### 1.3 Validate and activate

**Validate against target** checks:

- every bound node and input still exists in the graph (missing = **fail**, never
  guessed);
- an `output_node` is bound;
- every node class exists in the running ComfyUI;
- every referenced checkpoint exists on the target.

Passing sets the version **validated**; failing sets **invalid** and lists the
reasons. **Activate** makes a validated version selectable in Produce. **Retire**
stops new requests while preserving historical receipts.

### 1.4 Endpoint-capable workflows

A workflow supports first/last video only if it declares both `first_frame_asset`
and `last_frame_asset` bindings. Produce refuses endpoint-conditioned motion on a
workflow that lacks them — there is **no silent start-only fallback** (§3.4).

vid-build ships **two** bundled, human-authored packages:

- **SDXL Text to Image** (`sdxl-txt2img`, task `image`) — the default
  still-image package.
- **Wan First/Last Frame to Video** (`wan-flf-video`, task `video`) — Wan 2.2
  TI2V 5B with the umt5 text encoder and `wanBlockSwap` for 24 GB cards. It
  declares the endpoint bindings, so it drives endpoint-conditioned motion. The
  produced asset is stored as a **video** (`video/webm`) and thumbnailed from its
  first frame with FFmpeg.

Both packages are validated against the running ComfyUI when you press **Validate
against target**. The Wan package is verified against ComfyUI 0.35.0 (a live
integration test submits the exact bound graph and checks the returned WebM
stream).

---

## 2. Model profiles and execution targets

### 2.1 Model profiles

**Workspace settings → Model profiles** (`/w/{workspace}/model-profiles`): create
named profiles (name, task, checkpoint) chosen from checkpoints ComfyUI reports;
delete ones you no longer need.

- Set a **project default** on the project page (Project → Default model
  profile).
- The **generate form** lists profiles; picking one records it on the request,
  and the profile's checkpoint wins over the free-text checkpoint field.

### 2.2 Execution targets

**Compute → Execution targets** (`/w/{workspace}/execution-targets`): list the
built-in **Local ComfyUI** target and record additional targets (remote agent,
hosted ComfyUI, commercial API). Only metadata and pricing assumptions live here
— never raw secrets. Targets marked with measured observations show their
average.

---

## 3. Produce — generation

**Produce** (`.../videos/{video}/produce`) turns ready work into controlled
requests.

### 3.1 Ready work table

Each shot row shows readiness and which roles are ready (start / end / motion),
with a **Start / End / Motion** button that opens the generation form for that
role.

### 3.2 The generation form

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

For motion requests the form also exposes **frame count** and **fps** so the
video length is explicit. The form shows live ComfyUI reachability; if ComfyUI is
unreachable the **Queue** button is disabled and the reason is shown.

### 3.3 What happens on submit

Submitting **freezes** the current specification revision into a **generation
request** and creates one immutable **attempt per candidate**, then a durable
**job**. You are taken to the job page.

If you chose *Campaign later*, no job is queued; the request waits under
**Compute**.

### 3.4 Endpoint-conditioned motion (important)

For a **motion** request, the form offers **"Require first + last frames"**. When
checked, the request is refused unless:

1. both endpoints are **selected**;
2. both are **approved** (or **exception-approved**);
3. the chosen workflow declares `first_frame_asset` and `last_frame_asset`
   bindings.

When it runs, the worker stages both frames to ComfyUI and records **both
checksums** in the receipt. The job page's **Endpoints** column proves it.
vid-build will not quietly degrade a two-endpoint shot to a start-only render — a
start-only clip is a different, deliberate request (uncheck the box).

---

## 4. Super Shot — 2.0 to 30.0 seconds

A **Super Shot** (`.../videos/{video}/duration` or
`.../videos/{video}/super-shots/new`) is one perceived shot of **2.0–30.0
seconds**. It is generated **directly** (one invocation) when the selected
workflow can represent the whole duration, or as an ordered chain of adjacent
segments. A chain of `N` segments persists `N+1` ordered **anchors**; adjacent
segments share their boundary anchor and, once rendered, its exact image:

```text
A → [Segment 1] → B → [Segment 2] → C → [Segment 3] → D
```

`A` and `D` are the supplied, approved start and end images. `B` and `C` are
intermediate anchor images the app proposes; you review and edit their states and
each interval's movement prompt **before** any generation. The shot's own
`duration_target_ms` is the default; 5, 15 and 30 seconds are just presets.
Duration and frame rules come from the selected **workflow's declared
capabilities**, not from application constants.

### 4.1 Requirements

- A shot whose **start and end images are selected and approved** (or
  exception-approved). These anchor the first and last anchors.
- A **workflow that declares** `first_frame_asset` and `last_frame_asset`
  bindings **and** a `video_timing` capability block (frame rule, frame bounds,
  supported FPS and direct maximum). A workflow without timing capability is
  rejected server-side.

### 4.2 The flow

1. Open **Super Shot** and confirm or override the duration; choose a workflow,
   strategy (Auto/Direct/Chained), the movement prompt and the negative prompt.
2. **Create plan** persists a draft: anchors, segments and their movement
   prompts. **Nothing is generated yet.**
3. Review the plan: each anchor's visible state and each interval's movement
   prompt are shown and editable.
4. **Approve plan & generate** queues missing intermediate images, then the
   segments whose two anchors are ready.
5. Inspect the assembled video and retry only a weak anchor or segment.

### 4.3 What stays honest

- Intermediate images and bounded actions are visible and editable before
  generation; generation does not start before approval.
- Replacing an intermediate anchor invalidates only the segments that touch it.
- Every segment resolves its **own** adjacent anchors; an intermediate segment
  never falls back to the shot's outer endpoints.
- Motion context (**Previous frames** / **Native latent**) is present in the
  schema but **unavailable** until a verified workflow combines continuation with
  the supplied last image. Anchor-only (Shared images) is the supported mode.
- Assembly concatenates each segment's frames once, measures the actual duration
  and fails if it differs from the plan by more than one output frame plus
  container rounding. Visible seams are expected; the guarantee is one correctly
  assembled, playable video with every segment recoverable and inspectable.

### 4.4 Assisted planning

Leave the intermediate states blank and the configured **LM Studio** planner
proposes concrete boundary states and one movement per interval. If no planner is
configured (or it fails), the draft is created **visibly incomplete**: every
proposed state is prefixed `PLACEHOLDER:` and **approval is refused** with the
real reason until you describe them. Placeholder prompts are never queued.

You can always type the states and movements yourself, or edit the planner's
proposals before approving. Each edit saves cheaply while the plan is a draft;
once production has begun, editing a movement prompt marks that output **stale**
and invalidates the assembly rather than silently reusing the old prompt.

### 4.5 Intermediate images and references

Intermediate images are generated by a **separate image workflow** you choose
(defaults to the first image package). The **image model profile** is used for
anchors, never the video checkpoint. When that image workflow declares a
reference input, the app stages the supplied endpoints and the previous accepted
anchor as conditioning references; if it declares none, anchors are text-only and
the app does not claim references reached the graph.

You can also **attach** an existing project image to any intermediate anchor;
attaching invalidates only the segments that touch that boundary.

### 4.6 Review, retry, resume and hand-off

- **Select a take** — choose a different attempt's output for a segment;
  downstream work in context mode is marked stale.
- **Retry** — a retry creates a **new attempt** and job; the old terminal job and
  asset are preserved as history.
- **Resume** — queue ready work in dependency order after a failure.
- **Join review** — record accept / reject / notes for each boundary. This is a
  human note, not an automated quality claim.
- **Use as the shot's video** — selects the assembled video as the shot's motion
  candidate so it enters the ordinary **Review / Edit / Export** flow.
- **Cancel** — stops future dispatch; work already running is retained as
  history and cannot un-cancel the plan.

### 4.7 Motion context (unavailable)

**Previous frames** and **Native latent** are disabled. No verified ComfyUI graph
combines continuation with the supplied last image; see
[the blocker report](../plans/super-shot-context-blocker.md). A schema column is
not an adapter and is not presented as one.


---

## 5. Watching a job

**Job page** (`.../jobs/{job}`) auto-refreshes and shows:

- **State** — queued → claimed → running → succeeded / failed.
- **Events** — a live timeline: validate, stage inputs, load models, submit,
  generate, collect, verify, persist, cleanup, plus **memory coordination**
  lines.
- **Attempts** — one per candidate with status, seed, model, duration,
  **endpoints** (for motion) and any error.
- **Outputs** — the produced assets with thumbnails; video outputs render with
  native playback controls.

Every attempt is immutable. **Retry** produces a *new* attempt rather than
overwriting the failed one.

---

## 6. Review — select and approve

**Review** (`.../videos/{video}/review`) is your inbox. Candidates are grouped by
shot and role.

### 6.1 Choosing

On each candidate card:

- **Select** — makes this the current asset for that shot role.
- **Approve** — your accountable decision.
- **Reject** / **Exception approve** — require a **reason**.
- **Assess** — record a human assessment (pass / review / fail). Assessments are
  **advisory**; they never approve or reject for you.

Cards show *selected*, the current decision, and a **stale** badge when the shot
changed since the selection was made.

### 6.2 Candidate comparison

**Compare** (or **Compare candidates** on a shot) opens
`.../shots/{shot}/candidates/{role}`: full-size candidates in a grid, each with
its **receipt** — model, seed, workflow SHA, duration, checksum — and the same
select/approve/exception/assess actions. Judge at full size, not from a
thumbnail.

### 6.3 Annotations

The **Annotations** card collects review notes. Add an observation (optionally
attached to a shot) and **Resolve** it when addressed. Write actionable notes
("continuity: apron colour differs from shot 2"), not vague dislike.

> **The rule:** selection is not approval. A model assessment is not approval.
> Approval is a human decision, and it can be an **exception approval** with a
> recorded reason.

---

## 7. Edit and export

**Edit** (`.../videos/{video}/edit`) assembles your call sheet into one
deliverable.

### 7.1 Building the cut

- **Selected assets** (right column) lists every current selection; **Add** places
  it on the timeline.
- Each clip shows its asset, shot, duration and transitions.
- **Duration (s)** trims a clip; **In / Out** set **cut / fade / dissolve**;
  **Save** applies.
- **↑ / ↓** reorder; **Remove** takes a clip out of *this cut* — the asset is
  retained.

### 7.2 Validate

**Validate** checks for missing assets and missing checksums and reports warnings
you can resolve or accept.

### 7.3 Export

**Export** opens a profile summary (the video's resolution and frame rate) and
**Queue export**. The worker assembles a single **MP4** (H.264, `yuv420p`,
faststart) using FFmpeg.

- **Cut** transitions are rendered as a near-instant transition.
- **Fade** and **Dissolve** are composited with a crossfade so **every clip is
  preserved**.

The **export receipt** (`/exports/{id}`) plays the result, offers a **download**,
and shows the checksum — linking the finished film back to its source revision
and assets.

---

## 8. Compute planner and campaigns

**Compute** (`.../videos/{video}/compute`) answers: *where, when and at what
approximate cost should ready work run?*

### 8.1 Planning

- **Requests** shows work saved for planning ("campaign later").
- **Target comparison** shows each target with its **observed average** per
  candidate. Until you have measured runs it says *"assumed (no observations
  yet)"* and uses a default. Estimates are built from **your** measurements.

### 8.2 Campaigns

Create a **draft campaign** naming a target; it collects the waiting requests. On
the **campaign page**:

1. **Preflight** — verifies the target (ComfyUI reachable for local), that every
   item still has a workflow, and that nothing is missing. Passing marks it
   **ready**.
2. **Launch** — queues the jobs. A campaign will not launch in an invalid state.
3. **Pause** — stops claiming new items; running work continues.
4. **Cancel** — requests cancellation; produced assets and charges are preserved.
5. **Reconcile actuals** — records actual time/cost so future estimates improve
   without rewriting history.

Two GPUs are modelled as **independent workers**; VRAM is never summed.

---

*vid-build 0.02 — idea to finished film, one visible change at a time.*
