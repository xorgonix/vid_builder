vid-build 0.02 — Creative Direction, Storyboard and Animatic Change Specification

Status: Corrective implementation specification
Applies to: vid-build 0.01 / Mark 2 v3.1 implementation
Purpose: Preserve the strong production foundation while restoring the intended creator-first product direction
Primary stack: Go, Chi, templ, HTMX, daisyUI, SQLite, small JavaScript islands, LM Studio, ComfyUI, FFmpeg
Authoritative database: SQLite

1. Executive decision

vid-build must become a concept-to-animatic directing environment, not primarily a workflow launcher with story fields attached.

The creator develops a concept, expands it into an increasingly detailed narrative structure, asks an LLM to propose scenes, beats and shots, examines those proposals as inexpensive blocking sketches, adjusts timing and intensity in a playable animatic, and only then commits selected shots to expensive image and video generation.

The corrected product spine is:

Concept → Creative brief → Structure → Scenes → Beats → Shots → Blocking → Animatic → Production → Review → Edit → Export

The product must support movement in both directions. A creator may begin broadly and expand downward, or open any scene, beat or shot and revise details. A change at a higher level must identify affected descendants without silently replacing approved creative decisions.

The central working artifact is the enriched storyboard. It is simultaneously:

a visual outline;

a shot-planning surface;

a continuity map;

a timing and intensity model;

a cheap animatic;

a human-reviewable plan for AI or conventional production.

The desktop computer is the production mothership. A phone or tablet is a first-class creative controller and animatic surface. The phone does not need to perform expensive rendering. It develops, blocks, times, reviews and submits bounded work to the mothership.

2. What remains correct and must be preserved

Version 0.02 is an evolution, not a rewrite. Preserve these v0.01 decisions:

Go application with server-rendered templ/HTMX interface.

SQLite as the sole authoritative database.

Project and Video as separate concepts.

Wails as an optional host rather than the architecture.

Local LM Studio, ComfyUI and FFmpeg behind the Go application.

Human-authored ComfyUI workflows imported and bound semantically.

Independent start-frame, end-frame and motion specifications.

Deterministic SVG blocking before image generation.

AI proposes; humans selectively apply.

Immutable generation attempts and traceable asset lineage.

Explicit selection, assessment and approval.

Media outside SQLite with checksums and atomic placement.

Loopback-only model services and controlled LAN access.

Human-readable prompts and negative prompts as versioned creative material.

3. Problems this change must correct

3.1 Product-centre drift

The implementation has become strongest in workflow execution, administration and production lineage. Those are valuable, but the intended differentiator is the creator’s ability to develop and see the film before rendering it.

3.2 Missing dramatic beat layer

The current hierarchy connects scenes directly to shots. This loses the meaningful changes that make up a scene and makes shot planning less explainable.

3.3 Static storyboard rather than animatic

Start blocking, motion text and end blocking exist, but they cannot yet be played as a timed sequence. The creator cannot feel pacing before generated clips exist.

3.4 Shot continuity is modelled but not operational

shot_links exists in the schema, but no complete store, service, route or interface makes a previous ending state available to the next shot.

3.5 Phone access is not yet phone-first creation

LAN pairing and responsive pages exist, but the mobile experience is still a reduced desktop interface. It lacks rapid dictation, beat/shot cards, touch-friendly blocking, animatic playback and resilient draft handling.

3.6 Pacing and intensity are too shallow

Duration, scene pace and brief excitement exist. The product cannot yet model or display dramatic intensity, visual intensity, tension, information density or emotional direction over time.

3.7 Specification and implementation mismatch

Several features are described as implemented more fully than the code supports, including pointer-based blocking, continuity board mode, endpoint media on storyboard cards and comprehensive role controls.

4. Product principles

4.1 Expand progressively

Every level answers a different question:

Level

Question

Concept

What is this and why should anyone care?

Brief

For whom, toward what reaction, under what constraints?

Structure

What major movement carries the audience from beginning to end?

Scene

What happens in one dramatic, explanatory or time/location unit?

Beat

What changes, and why does it matter?

Shot

What must the audience see to experience that change?

Blocking

Where are subjects, camera, props, gaze and movement?

Animatic

Does the sequence work in time?

Production

Which model, workflow or conventional method should realize it?

The creator can stop at any useful level. A local-business advertisement may need only a brief, three scenes and eight shots. A dramatic short may use sections, scenes, beats, dialogue and continuity inheritance.

4.2 Detail without prompt dumping

Higher-level context is authoritative source material, not text automatically pasted into every model request. Each planner or generator receives a bounded, purpose-built context projection.

4.3 State, not pixels, crosses a cut

The next shot inherits relevant semantic state from the previous shot:

character identity and appearance;

costume and condition;

props and their state;

location and time;

physical placement when required;

emotional state;

knowledge and intention;

lighting direction and weather when required.

Composition, camera angle and focal length may change. Pixel inheritance occurs only when the transition explicitly requires continuous image-to-video action or a match.

4.4 Cheap decisions precede expensive decisions

The normal progression is:

text;

blocking;

timed blocking animatic;

concept frames;

final endpoints;

motion generation;

enhancement and export.

The application must never make final-quality rendering the default response to an unresolved creative question.

4.5 Explainable AI collaboration

An LLM proposal must say what each scene, beat or shot accomplishes. The user may accept individual items, edit them or reject them. Applying one item must not require applying the entire proposal.

5. Corrected domain hierarchy

Workspace
└── Project
    ├── Creative world
    │   ├── Characters
    │   ├── Locations
    │   ├── Props
    │   ├── Style rules
    │   └── Source materials
    └── Video
        ├── Creative brief
        ├── Sections (optional)
        │   └── Scenes
        │       └── Beats
        │           └── Shots
        │               ├── Start specification
        │               ├── End specification
        │               ├── Motion specification
        │               ├── Blocking
        │               ├── Dialogue/audio cues
        │               └── Production assets
        ├── Shot transitions and continuity handoffs
        ├── Animatic revisions
        └── Final edits and exports

5.1 Implicit nodes for simple projects

Complexity must be progressive:

Every video may have an implicit root section.

A scene may have one implicit beat.

The UI hides implicit containers unless the user requests more structure.

Existing v0.01 scenes migrate with one implicit beat containing their existing shots.

This preserves a simple experience while giving narrative work a correct ontology.

6. New and revised data model

All migrations must be forward-only, transactional and tested against a copy of a representative v0.01 database.

6.1 beats

Add:

CREATE TABLE beats (
    id                    TEXT PRIMARY KEY,
    scene_id              TEXT NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    title                 TEXT NOT NULL,
    purpose               TEXT NOT NULL DEFAULT '',
    trigger               TEXT NOT NULL DEFAULT '',
    action                TEXT NOT NULL DEFAULT '',
    reaction              TEXT NOT NULL DEFAULT '',
    resulting_state       TEXT NOT NULL DEFAULT '',
    intended_audience_effect TEXT NOT NULL DEFAULT '',
    duration_budget_ms    INTEGER NOT NULL DEFAULT 0,
    position              INTEGER NOT NULL DEFAULT 0,
    implicit              INTEGER NOT NULL DEFAULT 0 CHECK (implicit IN (0,1)),
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    revision              INTEGER NOT NULL DEFAULT 1
) STRICT;

Add beat_id to shots. During migration:

Create one implicit beat per existing scene.

Attach every existing shot to its scene’s implicit beat.

Make beat_id required after backfill.

Retain scene_id temporarily only if required for staged migration; thereafter derive scene through beat.

6.2 Narrative metrics

Add versionable metrics at scene, beat and shot scope:

CREATE TABLE narrative_metrics (
    id                  TEXT PRIMARY KEY,
    video_id            TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    entity_type         TEXT NOT NULL CHECK (entity_type IN ('scene','beat','shot')),
    entity_id           TEXT NOT NULL,
    dramatic_intensity  INTEGER CHECK (dramatic_intensity BETWEEN 0 AND 100),
    visual_intensity    INTEGER CHECK (visual_intensity BETWEEN 0 AND 100),
    tension             INTEGER CHECK (tension BETWEEN 0 AND 100),
    information_density INTEGER CHECK (information_density BETWEEN 0 AND 100),
    emotional_valence   INTEGER CHECK (emotional_valence BETWEEN -100 AND 100),
    notes               TEXT NOT NULL DEFAULT '',
    version             INTEGER NOT NULL,
    created_by_user_id  TEXT NOT NULL REFERENCES users(id),
    created_at          TEXT NOT NULL,
    superseded_at       TEXT
) STRICT;

Metrics are creative guides, not scientific claims. Empty metrics remain valid.

6.3 Dialogue and timed cues

Add:

CREATE TABLE shot_cues (
    id                 TEXT PRIMARY KEY,
    shot_id            TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    cue_type           TEXT NOT NULL CHECK (cue_type IN
                         ('dialogue','voiceover','sound','music','caption','action')),
    character_id       TEXT REFERENCES characters(id),
    start_ms           INTEGER NOT NULL DEFAULT 0,
    duration_ms        INTEGER NOT NULL DEFAULT 0,
    content            TEXT NOT NULL DEFAULT '',
    delivery           TEXT NOT NULL DEFAULT '',
    subtext            TEXT NOT NULL DEFAULT '',
    settings_json      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings_json)),
    position           INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    revision           INTEGER NOT NULL DEFAULT 1
) STRICT;

Version 0.02 needs browser speech synthesis or silent caption playback only. Generated voice production remains optional and may be deferred.

6.4 Shot transitions and continuity handoffs

Replace the passive shot_links.metadata_json convention with validated structured content. Existing rows remain compatible.

Required transition types:

continuous_action

hard_cut

reaction_cut

point_of_view_cut

cutaway

match_action

match_composition

dissolve

time_jump

location_change

parallel_action

audio_led_cut

Each sequential shot connection stores:

{
  "transition_type": "reaction_cut",
  "duration_ms": 0,
  "motivation": "The second cup causes Elena's suspicion",
  "inherit": {
    "character_identity": true,
    "costume": true,
    "physical_condition": true,
    "emotional_state": true,
    "knowledge_state": true,
    "prop_state": true,
    "physical_positions": false,
    "location": true,
    "lighting": true,
    "weather": true
  },
  "recompose": {
    "allowed": true,
    "shot_size": "close",
    "angle": "reverse",
    "camera_side": "preserve_screen_direction"
  },
  "notes": "Do not copy the previous composition"
}

Add store and service methods for create, update, list, delete and validate. Add optimistic revision control if links become mutable aggregates.

6.5 Continuity state snapshots

Add a semantic state document for each shot endpoint:

CREATE TABLE shot_states (
    id                 TEXT PRIMARY KEY,
    shot_id            TEXT NOT NULL REFERENCES shots(id) ON DELETE CASCADE,
    endpoint           TEXT NOT NULL CHECK (endpoint IN ('start','end')),
    version            INTEGER NOT NULL,
    state_json         TEXT NOT NULL CHECK (json_valid(state_json)),
    source             TEXT NOT NULL CHECK (source IN ('human','model','derived','mixed')),
    created_by_user_id TEXT REFERENCES users(id),
    created_at         TEXT NOT NULL,
    superseded_at      TEXT,
    UNIQUE (shot_id, endpoint, version)
) STRICT;

State JSON may include characters, props, setting, time, weather, emotion, knowledge and open continuity questions. Stable IDs must be used wherever entities exist in the project bible.

The end state of Shot A may be proposed as the start state of Shot B according to the transition handoff. It must remain a proposal until applied.

6.6 Animatic revisions

Add:

CREATE TABLE animatics (
    id                 TEXT PRIMARY KEY,
    video_id           TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    source_revision    INTEGER NOT NULL,
    settings_json      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(settings_json)),
    status             TEXT NOT NULL DEFAULT 'draft'
                         CHECK (status IN ('draft','review','approved','stale')),
    created_by_user_id TEXT NOT NULL REFERENCES users(id),
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    revision           INTEGER NOT NULL DEFAULT 1
) STRICT;

An animatic references the ordered narrative structure rather than duplicating it. Its settings include playback speed, placeholder voice choices, captions and display mode.

7. Blocking schema version 2

7.1 Stable element identity

Every actor, prop, label and arrow receives a stable element ID. Actors reference character_id whenever possible. Comparison and interpolation must match by ID, never display name alone.

7.2 Endpoint schema

Each endpoint includes:

canvas/aspect;

camera frame, height, angle and focal category;

subjects with stable IDs;

pose vocabulary plus optional free-text pose;

facing and gaze target;

foreground/midground/background placement;

props with state;

environment cues;

production notes.

7.3 Motion schema

Motion becomes structured in addition to prompt text:

{
  "subject_motions": [
    {
      "element_id": "actor_elena",
      "action": "turns toward doorway",
      "start_ms": 1100,
      "duration_ms": 900,
      "easing": "fast_out"
    }
  ],
  "camera_motion": {
    "kind": "push_in",
    "amount": "subtle",
    "start_ms": 0,
    "duration_ms": 3500
  },
  "environment_motion": ["curtain moves gently"],
  "negative_motion": ["no camera orbit", "no costume change"]
}

7.4 Direct manipulation

Implement touch and pointer interaction as a bounded JavaScript island:

tap/select element;

drag actor, prop, gaze target and camera frame;

add/remove elements;

duplicate from opposite endpoint;

align and distribute;

mirror composition;

lock element;

undo/redo for unsaved local edits;

explicit Save creating a new version;

property table remains the accessible canonical alternative.

Coordinates must clamp to valid ranges. Server-side schema validation remains authoritative.

8. Planner behaviour and contracts

8.1 Context builder

Replace ad hoc concatenation with named context projections:

brief_context

scene_context

beat_context

shot_context

continuity_context

visual_style_context

workflow_prompt_guidance

Every LLM call records which projections and source revisions it received. Apply configurable character/token budgets per projection. Do not send an entire project history to a shot-level call.

8.2 Structure proposal

The structure planner proposes sections and scenes only. It must include purpose, audience effect, duration budget and the change from scene opening to scene ending.

8.3 Beat proposal

For one selected scene, propose beats with:

title;

purpose;

trigger;

action;

reaction;

resulting state;

audience effect;

duration budget;

suggested narrative metrics.

8.4 Shot-breakdown proposal

For one selected beat, propose the minimum shots required to communicate it. Each proposed shot must include:

narrative purpose;

what the audience must notice;

start state;

visible change;

end state;

framing and point of view;

approximate duration;

transition into and out of the shot;

continuity requirements;

suggested generation method (blocking_only, generated, stock, live_action, composite, still_with_motion);

justification for why the shot exists.

The planner must be explicitly allowed to recommend one shot when one shot is enough.

8.5 Continuity proposal

When Shot B follows Shot A, the planner receives:

Shot A accepted end state;

the transition type;

Shot B purpose;

project bible invariants.

It proposes Shot B’s start state and identifies inherited, intentionally changed and unresolved facts. It must not copy composition unless requested.

8.6 Proposal discipline

All planner outputs use versioned JSON schemas. Validate before storage. Invalid results are retained as failed proposal evidence, not silently coerced into authoritative data.

The UI must support:

apply all;

apply selected scenes/beats/shots;

edit before application;

reject with optional reason;

regenerate from explicit feedback;

compare proposal against current structure.

9. Prompt compiler correction

9.1 Authority versus rendering

Structured story, state, blocking and continuity data are authoritative. Model prompts are compiled projections for a specific workflow and model family.

9.2 Layer model

Locking is orthogonal to compilation mode. Remove ModeLock as a textual mode.

Each layer has:

type Layer struct {
    Scope    string
    Mode     string // inherit, append, replace, suppress
    Positive string
    Negative string
    Locked   bool
    Priority int
}

A locked layer still contributes text. Lock only prevents automated revision without explicit human authorization.

9.3 Required compiled products

The compiler produces separate fields when supported:

first-frame positive and negative;

last-frame positive and negative;

motion positive and negative;

reference/identity guidance;

workflow-specific auxiliary fields.

For a workflow with a single prompt input, the compiler emits an explicit lossy merge report. It never silently pretends independent fields remain independent.

9.4 Wan-oriented shot compilation

For Wan-style video generation, use this ordering:

subject and setting → initial state → action/change → ending state → secondary motion → shot/camera → lighting/colour → style/mood → continuity constraints

The end prompt must be derived from the accepted end state, not produced by lightly rewriting the start prompt.

9.5 Prompt linting

Warnings include:

start and end prompts identical or near-identical;

no visible state change;

contradictory camera commands;

conflicting lighting or colour instructions;

too many primary actions;

continuity invariant changed without authorization;

prompt exceeds selected model/workflow budget;

important structured fact was dropped during lossy compilation.

Lint warnings are advisory except for invalid workflow bindings or missing required fields.

10. Enriched storyboard interface

10.1 Hierarchical navigation

The board supports collapsible:

Section
  Scene
    Beat
      Shot

Simple projects may hide Section and Beat labels while retaining them internally.

10.2 Shot card

Every shot card shows:

shot title, purpose and duration;

parent beat and scene;

start endpoint;

visible change/motion;

end endpoint;

transition in/out;

continuity warnings;

dialogue/cues;

readiness and stale state;

production method;

selected assets when available.

Endpoint display preference:

selected final frame;

selected concept frame;

current blocking SVG;

empty-state prompt.

10.3 Board modes

Implement and test:

Compact: overview and ordering.

Director: large start/change/end representation.

Continuity: transitions, inherited state, screen direction and discrepancies.

Pacing: durations, dialogue and narrative metric curves.

Production: readiness, attempts, failures, estimates and approvals.

10.4 Scene and beat inspector

A creator can open a scene or beat, discuss its purpose with the planner, request alternatives, view its children and see how its start/end state connects to neighbouring units.

Do not force the creator to enter each shot inspector to understand the sequence.

11. Animatic

11.1 Minimum viable animatic

The browser must play the whole video or selected scene/beat without generated assets.

For each shot:

Display start blocking.

Hold or interpolate simple supported properties toward end blocking.

Display captions/dialogue cues at their time offsets.

Optionally speak placeholder dialogue using browser speech synthesis.

Apply the selected transition.

Advance according to target duration.

Unsupported motion remains represented by arrows and captions. The product must not imply that blocking playback predicts final model motion.

11.2 Playback controls

play/pause;

restart;

scrub by time;

previous/next shot;

loop shot/beat/scene;

playback speed;

captions on/off;

placeholder voice on/off;

show purpose/metrics overlay;

full-screen presentation.

11.3 Timing edits

The creator can adjust shot duration and cue timing during playback. Changes save with optimistic revision checks and immediately update total scene/video duration.

11.4 Metric curves

Display dramatic intensity, visual intensity, tension, information density and emotional valence across time. The creator may show one or multiple curves, drag values, or leave them unset.

The curves are planning tools. They do not automatically change shots unless the creator requests a proposal.

12. Mobile and PWA direction

12.1 v0.02 connected mobile requirement

The paired phone interface must support the complete inexpensive creative loop:

capture or dictate concept notes;

discuss the concept;

view and apply scene/beat/shot proposals;

reorder scenes, beats and shots;

edit purposes, durations and metrics;

edit blocking using touch or property fields;

play the animatic;

annotate and approve work;

submit production requests to the mothership;

inspect job status and candidates.

Expensive model execution remains on the desktop or configured execution target.

12.2 Installable shell

Add a web manifest, icons, install metadata and a minimal service worker. Cache only the application shell and safe static assets initially.

12.3 Offline drafts

Offline operation is limited to explicit draft types:

concept notes;

scene/beat/shot text edits;

blocking drafts;

annotations.

Store drafts in IndexedDB with:

entity ID;

base server revision;

locally generated draft ID;

timestamp;

mutation type;

payload.

On reconnection, never silently overwrite a newer server revision. Present a three-way conflict view: base, server current and mobile draft.

Production, approvals, deletions, membership changes and workflow configuration require a live authoritative server connection.

13. Security and correctness repairs

These repairs are release blockers.

13.1 Route authorization

Apply role middleware or equivalent service-level authorization to every mutation:

Owner: membership, services, targets, destructive administration.

Editor: creative and production mutations.

Reviewer: annotations, assessments and assigned approvals only.

Viewer: read only.

Authorization must be tested for every route family. CSRF protection does not replace authorization.

13.2 Specification cleanup

Remove accidental conversation fragments and typographical corruption from the v3.1 specification. Update claims that exceed implemented behaviour.

13.3 Structured validation

Validate blocking against an explicit versioned schema.

Reject NaN, infinity and out-of-range coordinates.

Validate transition handoff documents.

Validate planner JSON before proposal creation.

Validate all referenced entity IDs belong to the same project/video scope.

13.4 Stable identity

Continuity comparison uses entity and element IDs. Names are presentation only.

14. Application routes and services

Add or complete these route families. Exact prefixes should follow the existing video base.

14.1 Beats

POST   /scenes/{sceneID}/beats
PATCH  /beats/{beatID}
DELETE /beats/{beatID}
POST   /beats/{beatID}/move
POST   /beats/{beatID}/propose-shots

14.2 Narrative proposals

POST /scenes/{sceneID}/propose-beats
POST /beats/{beatID}/propose-shots
POST /shots/{shotID}/propose-continuity

14.3 Shot transitions and states

GET    /shots/{shotID}/state
PATCH  /shots/{shotID}/state/{endpoint}
GET    /shots/{shotID}/transition-out
PUT    /shots/{shotID}/transition-out
POST   /shots/{shotID}/transition-out/derive-next

14.4 Cues

POST   /shots/{shotID}/cues
PATCH  /cues/{cueID}
DELETE /cues/{cueID}
POST   /cues/{cueID}/move

14.5 Animatic

GET   /animatic
GET   /animatic/data
PATCH /animatic/settings
POST  /animatic/approve

/animatic/data returns a bounded, authorized playback document containing current blocking, timing, transitions and cues—not production credentials or filesystem paths.

15. Invalidation rules

Changes must mark affected descendants stale without deleting them.

Change

Stales

Brief visual style

Dependent prompts and unapproved visual assets

Scene purpose

Beat proposals and shot plans derived from old revision

Beat resulting state

Child shot plans and next-beat continuity proposals

Shot start state

Start prompt/frame and dependent motion

Shot end state

End prompt/frame, dependent motion and next-shot start proposal

Transition/handoff

Next-shot derived state and continuity assessment

Blocking

Corresponding endpoint concepts/finals and animatic snapshot

Duration

Cue timing warnings, animatic and edit duration totals

Dialogue cue

Animatic audio/captions; generated audio if present

Prompt compiler version

Compiled previews only until user requests regeneration

Approved assets remain visible with a stale explanation. Nothing approved is silently replaced.

16. Testing requirements

Version 0.02 is not complete until tests cover the product’s decision boundaries.

16.1 Unit tests

beat validation and ordering;

blocking v2 parsing, validation, comparison and interpolation;

continuity handoff merge rules;

stable-ID matching;

prompt compilation and lock semantics;

prompt linting;

transition validation;

narrative metric bounds;

animatic timing calculations;

cue overlap and duration warnings.

16.2 Store tests

v0.01-to-v0.02 migration with existing scenes and shots;

implicit beat creation;

optimistic revision conflicts;

version supersession;

cross-project ID rejection;

cascade behaviour;

stale propagation;

shot link CRUD;

backup/restore includes new tables.

16.3 Authorization matrix tests

For every mutation family, test owner, editor, reviewer, viewer, non-member and unauthenticated users. Include direct HTTP requests; hiding a button is not authorization.

16.4 Planner contract tests

Using deterministic fixture responses:

valid proposal accepted;

malformed JSON rejected and recorded;

partial application creates only selected entities;

existing approved data not overwritten;

context projection respects limits;

shot proposal includes visible change and justification;

continuity proposal distinguishes inherited and recomposed facts.

16.5 Web integration tests

concept → brief → scene → beat → shot path;

paired blocking save/reload;

touch/property edit payload equivalence;

animatic data reflects current revisions;

duration changes update totals;

endpoint asset precedence on board cards;

offline draft conflict handling;

production request remains impossible without required approved inputs.

16.6 End-to-end acceptance scenario

Automate one small advertisement:

Create a project and 30-second video.

Enter a local-business concept.

Propose and selectively apply a brief.

Propose three scenes.

Expand one scene into two beats.

Propose and apply a minimal shot breakdown.

Generate start/end blocking.

Edit blocking manually.

Define continuity between two shots.

Add dialogue and sound cues.

Play the complete blocking animatic.

Adjust duration and intensity.

Generate concept endpoints through a fixture workflow.

Select and approve candidates.

Generate or import motion clips.

Assemble and export.

Verify the receipt and asset lineage.

17. Implementation phases

Phase 0 — Stabilize the base

Clean specification corruption.

Audit implementation claims against actual behaviour.

Fix role enforcement.

Add missing integration-test harness.

Fix prompt lock semantics.

Exit: All existing tests pass; authorization matrix for current routes passes; documentation no longer overclaims.

Phase 1 — Beats and narrative development

Add beats migration and implicit-beat compatibility.

Add beat store/services/routes/templates.

Split outline planning into structure, beat and shot proposals.

Add selective application and proposal diff.

Exit: A creator can expand one concept through scenes and beats into justified shots without editing JSON.

Phase 2 — Continuity state and shot links

Implement shot state snapshots.

Implement transition/handoff CRUD and validation.

Add continuity derivation proposal.

Add Continuity board mode.

Use stable IDs in blocking comparison.

Exit: The creator can carry Shot A’s ending state into a differently composed Shot B and inspect exactly what changed.

Phase 3 — Blocking editor v2

Introduce versioned schema and migration reader.

Add direct pointer/touch manipulation.

Add undo/redo, align, mirror, lock and validation.

Add structured motion data.

Exit: Start/end blocking can be comfortably created on desktop and phone without editing numeric JSON.

Phase 4 — Playable animatic

Add cues and timing.

Add browser player and simple interpolation.

Add scene/beat/shot looping and scrubbing.

Add metric curves and duration editing.

Exit: A complete video can be watched and evaluated as a timed blocking animatic before any image generation.

Phase 5 — Prompt and endpoint integration

Add target-aware prompt compilation.

Integrate Wan-oriented template and linting.

Display blocking/concept/final endpoint precedence.

Connect continuity state to prompt layers.

Exit: Approved narrative and blocking decisions compile into separate, inspectable start, end and motion prompts without near-clone endpoints.

Phase 6 — Phone-first connected workflow

Complete touch layouts.

Add dictation-friendly capture.

Add mobile proposal review and animatic playback.

Add production submission/status flow.

Add installable PWA shell.

Exit: A creator can develop and approve the inexpensive portion of a short video from a phone, then send bounded production work to the desktop.

Phase 7 — Resilient offline drafts

Add IndexedDB draft queue.

Add reconnection and three-way conflict UI.

Restrict offline actions to safe creative drafts.

Exit: Temporary loss of LAN does not lose permitted mobile draft work or silently overwrite server state.

18. Explicit non-goals for 0.02

Competing with DaVinci Resolve or Premiere.

Full skeletal character animation.

Predicting final generative-video motion from the animatic.

Real-time multi-user co-editing.

Autonomous production without human application and approval.

Exposing ComfyUI directly to phones or the public internet.

Building ComfyUI graphs in Go.

Full offline rendering from a phone.

Finished voice cloning or music generation.

Automatic rewriting of approved creative material.

19. Release acceptance criteria

vid-build 0.02 is acceptable only when all statements below are demonstrably true.

Creative development

A user can begin with a short concept and progressively expand it.

Scenes contain explicit beats, including implicit beats for simple work.

Every proposed shot states why it exists and what visibly changes.

Proposals can be selectively applied.

Higher-level revisions identify stale descendants.

Storyboard and continuity

Start and end blocking are independently editable.

Blocking elements use stable IDs.

Storyboard cards display the best available endpoint representation.

Sequential shots expose their transition and continuity handoff.

The next shot may inherit state while changing composition.

Continuity mode identifies unexplained changes.

Animatic

A complete blocking-only video can be played without ComfyUI.

Shot durations and transitions affect playback.

Dialogue/cues appear at their specified times.

The creator can scrub and loop a shot, beat or scene.

Pacing and selected narrative metrics are visible over time.

Prompting and production

Start, end and motion prompts remain separate when the workflow supports them.

Locked prompt layers still compile and resist automated edits.

The compiler reports lossy merges.

Near-identical start/end prompts produce a warning.

ComfyUI remains private and workflow graphs remain externally authored.

Phone workflow

A paired phone can create and revise concept, scenes, beats and shots.

A phone can edit blocking with touch-friendly controls.

A phone can play the animatic and submit approved production work.

Loss of connectivity cannot silently destroy work.

Security and reliability

Every mutation route has tested role authorization.

Cross-workspace and cross-project references are rejected.

All new structured JSON is schema validated.

Migration preserves existing projects and creates implicit beats.

Backups, verification and restore include all new state.

Unit, store, route and end-to-end suites pass from a clean checkout.

20. Definition of “solid”

The product is solid when it does not depend on the creator remembering what the application failed to model.

It must explicitly retain:

what the video promises;

what each scene accomplishes;

what changes in each beat;

why every shot exists;

what is visible at the start and end;

what happens between those endpoints;

what state passes to the next shot;

how long the audience experiences each moment;

where intensity rises, holds and releases;

which human accepted each creative or production decision;

which exact workflow, model, prompt and input produced each asset.

The creator should be able to begin with a vague thought, expand it into a structured film, inspect any level of detail, watch a cheap version, revise the reasoning, and commit expensive work only when the plan is coherent.

That is the corrected direction for vid-build.