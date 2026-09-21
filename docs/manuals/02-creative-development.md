# vid-build 0.02 — Creative development

*See the film before the pixels exist.*

This manual covers the inexpensive creative loop: the **Project Bible**,
**Develop**, **Outline** (including beats), the **Board** and **Blocking**,
**Continuity**, the **Animatic**, the **Prompt workbench** and the **Shot
inspector**. None of it requires an image model.

---

## Contents

1. [Project Bible](#1-project-bible)
2. [Develop — from ambiguity to a decision](#2-develop--from-ambiguity-to-a-decision)
3. [Outline — what the film says](#3-outline--what-the-film-says)
4. [Storyboard and blocking](#4-storyboard-and-blocking)
5. [Continuity — state across cuts](#5-continuity--state-across-cuts)
6. [The playable animatic](#6-the-playable-animatic)
7. [Prompt workbench](#7-prompt-workbench)
8. [Shot inspector](#8-shot-inspector)

---

## 1. Project Bible

**Project → Project Bible** (`/w/{workspace}/projects/{project}/bible`) holds the
reusable world of the project:

- **Characters** — names and descriptions/invariants (e.g. "Maya, ceramicist,
  clay-dusted apron").
- **Locations** — places and continuity notes.
- **Style rules** — project-wide visual rules ("warm film grain, shallow depth
  of field").

Style rules and the accepted brief's **visual style** become the *inherited
base* for the prompt workbench (see §7). Keep this page honest — it feeds every
shot.

---

## 2. Develop — from ambiguity to a decision

**Develop** (`.../videos/{video}/develop`) is where an idea becomes accepted
direction.

### 2.1 The two columns

- **Left** — the **Accepted brief** card and the **Proposals** list.
- **Right** — the **Discovery conversation**.

### 2.2 Recording the idea

Type into the composer ("A 20-second teaser for a handmade ceramic studio. Calm,
tactile, warm light.") and **Send**. Messages are append-only history: you never
lose what was said.

### 2.3 Asking the planner

**Ask planner** sends the conversation plus project context to the configured LM
Studio model and appends a planner reply. The planner **proposes and discusses**;
it cannot change your project. If LM Studio is unavailable the button is disabled
and an error is shown.

### 2.4 Proposing a brief

**Propose brief** asks the model for a **structured brief** (premise, theme,
audience, desired reaction, pace, excitement, music/sound intent, visual style,
camera language, production notes). It is stored as a *proposal* — nothing
changes yet.

### 2.5 Applying a brief proposal

Open the proposal from the list. Each field appears with a **checkbox** and an
editable text box. Tick only the fields you accept (edit the wording freely),
then **Apply selected fields**. A new versioned brief is written and the older
one is superseded. Use **Reject** to discard with a record.

### 2.6 Revising the brief by hand

**Revise manually** opens a form for every brief field. Saving creates a new
version — the previous direction is never destroyed. **Brief history** shows all
versions.

### 2.7 Proposing an outline

**Propose outline** produces a section → scene → shot structure. Apply it with
**Apply whole outline**; it creates the sections, scenes and shots (each shot
with start, end and motion specs). Reject if it does not read well.

> **Human authority:** proposals never silently rewrite your brief or outline.
> You choose the fields and you press Apply.

---

## 3. Outline — what the film says

**Outline** (`.../videos/{video}/outline`) shapes the narrative before visual
detail.

- Each **section** groups scenes and carries a purpose and duration budget.
- Each **scene** carries a synopsis, purpose, pace and audience reaction.
- Each **beat** is a dramatic unit inside a scene: title, purpose, trigger,
  action, reaction, resulting state, intended audience effect and a duration
  budget.
- Each **shot** row shows title, purpose, duration and a **readiness badge**.

Actions: **Add section** (dialog), **Add scene** under a section, **Add beat**
under a scene, **Add shot** under a beat, **↑/↓** to reorder, **Edit** to change
fields, **Delete** to remove. Deleting a section rehomes its scenes rather than
silently dropping them; a beat that still owns shots, or a scene's only beat,
cannot be deleted. **Open** a shot to reach the inspector.

### 3.1 Implicit beats

Simple projects are unchanged: a scene starts with one **implicit beat** that
holds its shots and is hidden. The first time you add an explicit beat, the
implicit beat is promoted so the full hierarchy becomes visible. Nothing is lost
and nothing is rewritten.

### 3.2 Proposing beats and shots separately

The planner is split so you decide at the right altitude:

- **Propose beats** (from a scene) asks for the scene's dramatic beats. The
  proposal page lets you untick beats, edit any field before applying, apply all,
  reject with an optional reason, or regenerate from explicit feedback.
- **Propose shots** (from a beat) asks for the *minimum* shots that communicate
  that beat — one shot is a valid answer. Each proposed shot carries its purpose,
  what the audience must notice, start/change/end states, framing, duration,
  transitions, continuity needs, a suggested generation method (`blocking_only`,
  `generated`, `stock`, `live_action`, `composite`, `still_with_motion`) and a
  justification. Applying a shot folds that guidance into the shot's treatment so
  nothing the planner explained is lost.

Proposal pages also show the **current structure** for comparison, and an open
proposal is marked **stale** (never deleted) when the scene, beat, video or brief
it was based on has changed since.

A shot's purpose should be a sentence a director would say out loud. If you
cannot name the visible change, the shot is not ready to produce.

---

## 4. Storyboard and blocking

### 4.1 The Board

**Board** (`.../videos/{video}/board`) is the visual centre of the app. Each card
shows **START · CHANGE · END** with readiness and duration.

- **Display modes:** *Compact* (small thumbnails), *Director* (large, default),
  *Production* (readiness emphasis), *Continuity* (transitions and inherited
  state).
- **Filters:** All, or by readiness (`incomplete`, `blocking_ready`,
  `concept_ready`, `endpoint_ready`, `video_ready`, `complete`).
- **Open** a shot or jump to its **Blocking** editor.

Blocking sketches render instantly as SVG — no model call, no cost.

### 4.2 The Blocking editor

**Blocking editor** (`.../shots/{shot}/blocking`) is a paired view: **start** and
**end** side by side with a **Detected change** panel between them.

Controls on each side:

- **Camera** — shot size (wide/medium/close/extreme close), angle
  (eye/low/high/dutch), focal category and camera height.
- **Actors** — stable element id, name, optional project-bible character id,
  position (x,y), pose category plus free text, facing, gaze target; **+ actor** /
  remove.
- **Props** — element id, name, state and position; **+ prop** / remove.
- **Arrows** and **labels** — movement or camera arrows, production notes.
- **Environment cues** — free-text cues (for example "curtain moves gently").

Blocking is schema **version 2**: every actor, prop, label and arrow carries a
**stable element id**, so comparison and interpolation match elements by id
rather than by display name. Older version 1 documents are upgraded **in memory**
and only rewritten to v2 when a human saves; stored rows are never silently
rewritten.

You can edit by **dragging directly on the sketch** (actors, props, gaze targets,
camera frame, arrows) as well as through the property table. The toolbar offers
add/remove, **mirror**, align, distribute, **lock** an element, and **undo/redo**
for unsaved edits. Coordinates clamp to the valid range and the server validates
the document on save.

**Change analysis** compares start and end and reports shot-size, angle, camera
position, actor position/pose/gaze/turn, props and arrows. If nothing changes it
tells you plainly: *"No visible change detected — the shot lacks a payoff."*

Every control has a **JSON fallback** (expand "Edit as JSON"): the same data can
be typed directly, so blocking still works with JavaScript disabled. Motion is
also structured (subject motions with element id/action/timing/easing, camera
motion, environment motion, negative motion) alongside the motion prompt.

### 4.3 Model-assisted blocking

**Suggest blocking** asks the planner for a structured start/end proposal,
rendered as two preview sketches. **Apply blocking** saves the proposals you tick
as blocking versions. Nothing is applied until you say so.

---

## 5. Continuity — state across cuts

**Continuity** (`.../shots/{shot}/state`) carries *state*, not pixels, across a
cut.

- **Start state** and **End state** are versioned semantic documents (characters
  with stable project-bible IDs, props, setting, time, weather, emotion,
  knowledge, open questions). Each save creates a new version; the previous one
  is superseded, never lost.
- **Transition into the next shot** stores one of twelve required transition
  types, an optional duration, the **motivation** that makes the cut caused by
  the previous shot, an **inherit** block (identity, costume, condition, emotion,
  knowledge, props, positions, location, lighting, weather), a **recompose**
  block (allowed, shot size, angle, camera side) and notes.
- **Derive next start state** proposes the following shot's start state from this
  shot's end state and the handoff. The proposal distinguishes what is
  **inherited**, what is **intentionally changed** and what is **unresolved**,
  and it never writes the neighbour's state until you apply it.

The **Board** gains a **Continuity** mode showing each shot's outgoing
transition, inherited facts, screen direction and warnings (no transition, no
motivation, no end state recorded).

---

## 6. The playable animatic

**Animatic** (`.../videos/{video}/animatic`) plays the whole film as timed
blocking before any image generation. For each shot it shows the start blocking,
holds or interpolates supported properties toward the end blocking, shows
dialogue/caption cues at their offsets, optionally speaks placeholder dialogue
with browser speech synthesis, and advances by the shot's target duration.

Controls: play/pause (space), restart, previous/next shot, scrub, playback speed,
loop shot/beat/scene, captions on/off, placeholder voice on/off, purpose/metrics
overlay and full screen.

- **Timing edits** — change a shot's duration inline; it saves with an optimistic
  revision check and the total updates immediately.
- **Cues** — add and edit dialogue, voiceover, sound, music, caption and action
  cues with start and duration in milliseconds.
- **Narrative metrics** — dramatic intensity, visual intensity, tension,
  information density and emotional valence, per shot. Any metric may be left
  unset. Metrics are planning guides, not scientific claims, and never change
  shots by themselves.
- **Approve animatic** records the animatic as approved.

Blocking playback represents intended timing and staging only. It does **not**
predict final model motion; unsupported motion stays represented by arrows and
captions.

---

## 7. Prompt workbench

**Prompt workbench**
(`.../shots/{shot}/prompt-workbench?kind=start_frame|end_frame|motion`) edits
creative instructions as **layers** — without exposing any node wiring.

### 7.1 Inherited context

The top card shows the **inherited base**: the accepted brief's visual style plus
active project style rules. This is automatically prepended during compilation.

### 7.2 Layers

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

### 7.3 Compile preview and save

**Compile preview** shows the exact positive and negative fields that will be
bound — *what the model will actually receive*. **Save as new specification
version** writes a new spec version carrying both the compiled prompts and the
source layers.

First-frame and last-frame text are stored independently. If you write identical
text for both endpoints, expect a lint warning: the shot must express a visible
change.

---

## 8. Shot inspector

**Shot inspector** (`.../shots/{shot}`) is the single place to examine everything
about a shot. Tabs:

1. **Intent** — title, purpose, treatment, duration.
2. **Start** — positive/negative prompts, structured spec, version.
3. **End** — same for the destination state.
4. **Motion** — subject, camera and environmental movement; negative motion.
5. **History** — specification versions.

Each tab offers:

- **Suggest with planner** — creates a proposal you review and apply (it never
  overwrites silently). Proposals of type `prompt_revision` appear in Develop.
- **Compare candidates** — jumps to the candidate comparison page for that role.
- **Prompt workbench** (header) — layered editing (§7).
- **Blocking editor** (header) — paired blocking (§4.2).

Saving a spec creates a **new version**; the previous version stays available to
any generation receipt that referenced it.

---

*vid-build 0.02 — idea to finished film, one visible change at a time.*
