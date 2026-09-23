# vid-build — Super Shot integration repairs

Date: 2026-09-23  
Baseline: `f9f9fff9-657d-41e0-b0e1-16ca55c195a3.txt`, latest supplied source snapshot  
Audience: coding agent implementing the next repair pass

## 1. Authority, scope and completion rule

Implement this document against the current checkout. It refines and supersedes conflicting instructions for the issues addressed here in `vid-build-super-shot-completion-contract.md`. Preserve the rest of that contract. Check actual code before editing; identify any findings already fixed in a newer checkout.

The existing Super Shot feature has valuable working structure: adjacent anchors, a real creative planner, separate movement and purpose, draft review, attempt-specific segment retry keys, rational FPS assembly and mandatory duration probing. Preserve those improvements.

The task is to complete the integration. A new field, helper, test fixture or disabled control does not satisfy a requirement unless the actual user route and production worker use it correctly.

**Completion requires a traceable path:** browser action → authorization → domain validation → transaction → immutable queued work → worker → provider result → guarded publication → correct UI/Edit behavior.

[Evidence limitation: this document is based on source inspection, not execution of the supplied snapshot. Reproduce defects with meaningful tests, and distinguish automated verification from real model-render evidence.]

## 2. What stays and what changes

| Preserve | Repair |
|---|---|
| Go, SQLite, Chi/templ, HTMX and existing styling | Existing domain/service/worker integration |
| Imported ComfyUI workflow graphs | Workflow selection, reference capabilities and input bindings |
| A→B, B→C, C→D adjacent-anchor model | Meaningful, reference-conditioned intermediate images |
| First image + last image + movement + negative prompt per segment | Immutable resolved inputs and validated output identity |
| Assisted planning and manual editing | Revision handling when edits occur during execution |
| Current durable queue | Jobs must carry the attempt/revision they represent |
| Historical attempts and assets | Explicit current selection and stale-result handling |
| Rational FPS and mandatory ffprobe measurement | Preserve them through all new repairs |
| Existing authorization and workspace separation | Close the attempt-selection relationship gap |
| Normal Review/Edit workflow | Correct Super Shot selection role and handoff |
| Honest unavailable labels | Enable context only when an actual compatible adapter works |

Do not rewrite the app, switch databases, remove approval/review, delete old assets, disable editing to hide concurrency bugs, or replace the required assisted-image path with compulsory manual image preparation. Keep manual attach/select as an option.

## 3. Mental model for this repair

A **plan** describes desired work. An **attempt** records one frozen execution of part of that plan. An **asset** is its output. A **selection** chooses an eligible asset for current use. These are different facts.

An attempt can finish successfully after its inputs have been superseded. In that case its output is valid history but is not the current answer. Do not erase its receipt or let its completion overwrite newer intent.

Similarly, possession of a valid attempt ID is not permission to select that attempt into any segment. Eligibility depends on ownership, segment membership, input compatibility and result type.

## 4. R1 — Make reference-conditioned anchor generation real

### Current evidence

In `internal/worker/video.go`:

- `anchorImageWorkflow` falls back to `pickImageWorkflow` if an explicit selected workflow cannot be loaded.
- `GenerateAnchor` computes reference bindings but permits rendering when reference capability is missing.
- A reference manifest flag alone does not prove a reference reaches the model's conditioning path.

### Required behavior

1. Persist an explicit image workflow version for automatic intermediate-image generation.
2. If that selected version is missing, inaccessible or incompatible, stop this operation with an actionable error. Do not silently select another workflow.
3. Define the reference contract: which endpoints and optional preceding-anchor references the selected graph consumes. Validate required bindings before dispatch.
4. For the normal continuity-preserving path, require the declared reference images to be staged and bound. Text-only generation must not be presented as this capability.
5. If text-only experimental generation remains available, make it a separately labelled, explicitly selected mode. Record the mode and show its continuity limitation before render.
6. Keep the image model profile separate from the video model profile.
7. Include concrete anchor state, relevant appearance/setting descriptions and continuity constraints. Staging an image does not make unrelated text adequate.
8. Validate output bytes as an actual decodable image with expected dimensions/format; a filename extension is not proof.
9. If a graph uses a previous generated anchor as a required reference, make that dependency explicit and wait for its selected asset. Do not vary references according to which parallel job happens to finish first.

### Tests

Use distinct valid image fixtures for first endpoint, last endpoint and predecessor. Use distinct graph inputs for distinct semantic bindings. Assert exact staged source-to-input mapping, not merely that JSON contains `uploaded_`.

Test missing selected workflow, missing reference binding, failed upload, incompatible profile, invalid output bytes and missing required predecessor. All must fail before falsely publishing a current anchor.

A mock graph proves plumbing only. A real example must demonstrate that reference conditioning preserves the intended subject/setting across intermediate images.

## 5. R2 — Freeze job inputs and guard publication against edits

### Current evidence

`EditSegmentAction` can mark a running segment stale without replacing its attempt ID. `SetSegmentSucceededIfCurrent` checks only that ID and then clears staleness. The old render can therefore overwrite the edit.

`RunAssembly` records an input hash but calls an unconditional success update. An old assembly can become current after a contributing segment changes. Anchor publication through `SetAnchorSucceeded` is also unconditional.

The worker dispatch still invokes `RunSegment` using an aggregate segment ID. A job key containing an attempt ID is insufficient if execution reloads whatever attempt happens to be current at claim time.

### Required model

An executable job identifies immutable work:

```text
job_id
operation
plan_id and approved plan revision
segment_id or anchor_id or assembly_id
attempt_id (or immutable assembly execution ID)
input_snapshot_id/hash
```

A segment snapshot includes resolved first/last asset IDs and checksums, movement/negative prompts, workflow version/graph hash, model profile/settings, rational FPS, dimensions, frame counts, seed and optional context dependency. Anchor snapshots include the selected reference assets and image workflow. Assembly snapshots contain ordered selected attempt/asset IDs and exact frame/trim ranges.

The worker must execute the job's snapshot, not mutable live plan fields. Persist provider submission identity promptly after acceptance so restart/reconciliation can find the same work.

### Required state behavior

1. Editing inputs increments an input revision or changes a canonical input hash. Do not use a revision that also changes merely because status/progress changes.
2. Atomically invalidate affected selections/outputs and current assembly eligibility.
3. Preserve the old attempt and its eventual result as history.
4. On completion, compare the expected attempt AND input revision/hash AND allowed parent state.
5. Publish only when all match. Otherwise record a successful-but-superseded attempt without changing current output or clearing stale flags.
6. Apply equivalent protection to anchors, segments, failure updates and assemblies. An obsolete failure must not mark newer work failed either.
7. Cancellation prevents new provider submissions and current publication. Retain in-flight results as history where cancellation cannot stop the external job.
8. Completion comparison and publication must occur atomically. A read followed by an unconditional update remains racy.

[Implementation option: use a transaction with a conditional update keyed by an immutable input revision and selected attempt. If no row matches, return a typed superseded outcome rather than treating it as provider failure.]

### Exact regression scenarios

**Segment:** start attempt A with movement “walk”; pause fake provider; edit movement to “run”; release A. A's asset is retained, but the segment stays stale/newly planned and must not appear current. Repeat for an anchor replacement during the render.

**Anchor:** start image attempt A; attach or select a different image B while A runs; release A. B remains selected and adjacent segments remain correctly invalidated.

**Assembly:** start assembly using X/Y/Z; pause exporter; change selected Y to Y2; release exporter. Old output is previous output only. New assembly uses X/Y2/Z.

**Job identity:** queue attempt A; create/select attempt B before A's queued job runs. A's job either executes A's frozen snapshot or is superseded before submission. It must never execute B under A's job identity.

**Cancellation:** cancel during render/assembly; late completion cannot publish current success or un-cancel the plan.

Run these through actual queue claiming with controllable fake adapters. Directly calling a store conditional update alone is not adequate coverage.

## 6. R3 — Validate selected attempt ownership and compatibility

### Current evidence

`internal/video/completion.go`, `SelectSegmentAttempt`, checks the destination segment, then retrieves assets for arbitrary `attemptID`. It does not prove the attempt belongs to that segment/request/workspace. It falls back to the first asset if no video-like result is found.

### Required behavior

Validate server-side in one domain operation:

- caller may edit this plan;
- plan belongs to addressed workspace/project/video;
- segment belongs to that plan;
- attempt belongs to that exact segment, not just the same generation request;
- attempt has a successful eligible output;
- asset belongs to the attempt and correct scope;
- asset is verified video, not a still with a convenient role label;
- input snapshot is compatible with the current segment inputs;
- expected selection revision still matches;
- cancelled/otherwise prohibited parent states are rejected.

Select the exact asset ID when an attempt has multiple outputs. Do not choose an arbitrary first asset. If selecting an old take requires restoring old inputs, make that an explicit revision-restoration action; never silently clear current staleness.

Add an explicit attempt-to-segment relation if the current schema cannot prove membership. Backfill only provable relationships; historical unknowns remain ineligible until reconciled. Do not guess from attempt number.

After a valid change, invalidate the assembly and context-dependent descendants transactionally. Unrelated segments remain current.

### Tests

Accept a correct same-segment take. Reject an attempt from another segment in the same request, another request, another project and another workspace. Reject failed attempts, image outputs, mismatched asset IDs, stale input snapshots and stale selection revisions. Verify that rejected operations alter no current selection or dependent status.

## 7. R4 — Complete the assembly-to-Edit handoff

### Current evidence

`SelectAssembledVideo` stores role `motion`. The Edit handler enumerates selected roles `video`, `start_frame`, and `end_frame`. `CreateSelection` stores the supplied role directly.

### Required behavior

Choose and document one canonical role for the finished shot clip, consistent with the existing Edit path. The smallest likely correction is to select completed assemblies under `video`, while retaining `motion` where it describes generation intent. Inspect all consumers before changing shared terminology.

Preserve existing selections with a forward-compatible alias or migration as appropriate. Do not delete history or duplicate active selections unpredictably. Verify approval rules still apply as designed; selecting a clip does not silently grant creative approval.

Eligibility requires a current successful measured assembly with an unchanged input snapshot. The UI must visibly label prior/stale outputs and must not present them as the current finished shot.

### End-to-end acceptance

Through authenticated HTTP: create/render or fixture a current assembled video → use the “select assembled video” action → open normal Edit → find that asset in available clips → add it → verify edit item points to the correct video and measured duration. Also verify stale assemblies cannot enter this current-output path without an explicit historical-selection workflow.

## 8. R5 — Deliver context support or identify the exact external blocker

The application still rejects `tail_frames` and `native_latent` modes. Preserve the honest status. Do not claim this task complete merely because schema and UI options exist.

Required investigation and implementation:

1. Inspect available ComfyUI nodes/models/workflows.
2. Identify a graph that consumes predecessor motion context AND respects the segment's supplied last image.
3. Run a two-segment proof in ComfyUI independently of the app.
4. Pin/export that graph and its dependencies, then implement semantic bindings and validated context artifacts.
5. Store producer attempt/asset checksums and compatibility metadata. Consumers must depend on the selected producer attempt.
6. Define output contract: new frames only, context prefix plus new frames, or complete extended video. Store exact keep ranges; avoid duplicated overlap frames.
7. Test dependency scheduling and downstream invalidation using the same immutable-work rules as R2.
8. Compare joins to anchor-only generation, recording actual results rather than promising invisible joins.

Do not switch to a continuation workflow that ignores the final image. Tail-frame and native-latent modes are distinct capabilities and must be reported separately.

If unavailable, deliver a blocker record with missing model/node/graph, what was inspected or attempted, actual error/output, and concrete setup needed. Finish R1–R4/R6 independently. Do not incur new API/rental charges without existing authorization.

## 9. R6 — Strengthen tests and finish status reporting

The current reference test's comment claims both endpoints reach the graph, but its fixture lacks a last-image binding and only asserts an uploaded string exists. Replace that assertion with exact relationships. Do not change assertions merely to make current behavior pass.

Also inspect these adjacent integration gaps:

- `RefreshPlanStatus` counts failed missing anchors as generating, and does not fully distinguish assembly failure from assembling. A terminal child failure must produce a truthful parent state and recovery action.
- The page view still derives video presence from an assembly asset pointer. Display current versus previous output using status plus snapshot validity, not pointer presence alone.
- `SelectAssembledVideo` must validate current snapshot, not only `status == succeeded`.
- Planner `SegmentDurationsMS` is used to count segments but the actual durations are not included in the shown model prompt. Supply interval times/durations so proposed action pacing has that context. Validate returned indices/order, not just array lengths.
- Legacy diagnosis must inspect pre-Super-Shot generation requests/segments too; iterating only new `super_shot_requests` cannot discover old rows that never had such a parent.

These are concrete checks within this repair scope. Confirm each against the current checkout and fix any still present.

### Required test inventory

| ID | Proof |
|---|---|
| A01 | Explicit incompatible/missing image workflow does not silently fall back |
| A02 | Distinct required image references reach correct graph inputs |
| A03 | Invalid/non-image output bytes are rejected |
| A04 | Old segment success/failure cannot overwrite edited inputs |
| A05 | Old anchor result cannot replace a newer selected image |
| A06 | Old assembly cannot publish after selected inputs change |
| A07 | Queued job executes its own attempt snapshot |
| A08 | Cancellation blocks submission/publication as specified |
| A09 | Attempt/asset selection rejects cross-segment and cross-scope IDs |
| A10 | Old incompatible take cannot silently clear staleness |
| A11 | Assembled clip appears and can be added in normal Edit |
| A12 | Failed anchors/assembly produce truthful parent status/errors |
| A13 | Planner receives durations and validates ordered indices |
| A14 | Legacy pre-Super-Shot requests remain visible and diagnosable |
| A15 | Real context graph plus endpoint conditioning, or documented blocker |
| A16 | Existing tests for chain, retries, FPS and mandatory probing still pass |

## 10. Implementation procedure

1. Read repository instructions and this entire document. Produce a brief R1–R6 file map and proceed with implementation.
2. Reproduce each confirmed failure with a targeted regression test before fixing it.
3. Implement explicit attempt membership and immutable execution inputs in a forward migration. Preserve historical records and make migration idempotent according to repository conventions.
4. Wire queue payloads, worker entrypoints and conditional publication. Do not stop at adding columns.
5. Enforce reference workflow selection and stage/validate the correct images.
6. Repair selection and Edit handoff; finish truthful status/previous-output display.
7. Investigate and implement context support using an actual compatible graph, or record the specific external blocker.
8. Run targeted tests followed by repository tests. Regenerate templ files through the existing toolchain.
9. Run real short, 15-second and 30-second renders when the local environment permits. Save playable outputs, measured timing, graph/model versions and join observations. If unavailable, mark GPU acceptance unverified explicitly.
10. Update guides and presentations only to describe behavior actually delivered.

Do not use a wholesale rewrite to avoid understanding the existing flow. Preserve working features and user data. Fix routines shared by other production paths carefully and exercise those paths in regression testing.

## 11. Definition of done and final report

Return a table for R1–R6 and A01–A16 with implemented, verified, blocked or not completed status. Include changed files, migrations, exact commands/results, skipped tests, real render evidence and remaining limitations.

The user must be able to tell which capabilities work today and what requires external setup. “Implemented with mocks” is not equivalent to “visually verified on ComfyUI.” A recorded input hash is not a concurrency guard unless publication checks it. An attempt ID inside a job key is not an immutable execution payload unless the worker executes that attempt.

[Suggested implementation discipline: maintain one checklist linking each requirement to a production call path and a regression test. This prevents completing the helper while leaving the real route on the old behavior.]

## 12. Paste this introduction into the coding-agent session

```text
Implement docs/specs/vid-build-super-shot-final-integration-repairs.md against
this checkout. It is the authoritative repair brief for the latest Super Shot
integration issues. Read the whole document and preserve the working features
listed in section 2.

Give a short R1–R6 file map, then implement without stopping at the plan.
Reproduce the failures, wire fixes through actual routes/queue/worker paths,
and prove the behaviors listed in A01–A16. Pay particular attention to immutable
job snapshots, late results after edits, attempt-selection ownership, required
image references, and the assembled-video handoff to Edit.

Do not count fields, helpers, comments or mocks as finished capabilities.
Implement context with a real compatible ComfyUI graph or report its exact
external blocker while finishing all independent repairs. Preserve user data
and output history. Finish with the requirement/test status report in section 11.
```
