Implement the Super Shot correction in vid-builder using
SPECS/vid-build-duration-super-shot-correction-spec.md as the
implementation contract.

Read the entire document first. Section 15 updates and corrects earlier
sections wherever they conflict. Inspect the current repository before
editing, especially the duration planner, segment worker, workflow bindings,
SQLite migrations, queue, assembly, UI, and tests.

The essential behavior is:

- A creator supplies a first image, last image, movement prompt, negative
  prompt, and desired shot duration of 2–30 seconds.
- Every generated segment still has its own first image, last image, and
  movement prompt. For a three-segment shot, use A→B, B→C, C→D.
  Never generate A→D three times.
- Intermediate images are shared boundary assets. The app proposes their
  visible states and the movement prompt for each interval; the creator can
  review and edit them before generation.
- Duration comes from the shot by default. 5, 15, and 30 seconds are presets.
  Frame limits and segment lengths come from the selected workflow.
- Add previous-video-frame or native-latent motion context only through a
  verified ComfyUI workflow that supports context AND the segment’s last
  image together. Do not claim native latent chaining works based solely on
  stored metadata or tail-frame support.
- Context-dependent segments execute in order. If a selected segment output
  changes, mark its downstream dependants stale. Anchor-only segments retain
  independent retry behavior.
- Assemble each contributed output frame once; measure actual duration and
  make joins available for review.

First, give me a short implementation map: files to change, migrations,
available ComfyUI capabilities, and any contradictions you found. Then carry
out the work. Do not stop after the map unless a required external model or
workflow is genuinely unavailable; continue implementing everything that
can be implemented and identify the exact remaining blocker.

Replace tests that endorse endpoint reuse. Add tests for the A→B→C chain,
workflow capability checks, frame accounting, context dependencies,
invalidation, retries, assembly, and successful web routes. Run the
repository test suite. Run real ComfyUI renders if this environment has the
needed models and graph; otherwise report that integration as unverified.
Do not describe a passing mock test as proof of visual continuity.

Update the user guide, route documentation, and presentations that describe
duration videos. In your final report, distinguish implemented behavior,
verified behavior, and remaining limitations. Include the migration,
changed files, test commands/results, and links or paths to any rendered
examples.
