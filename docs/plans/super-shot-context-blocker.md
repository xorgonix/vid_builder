# Super Shot motion context — blocker report

Status: **blocked** (contract §5.8). No verified ComfyUI graph combines
predecessor motion context with terminal-image conditioning.

Date: 2026-09-23 · ComfyUI inspected live at `http://127.0.0.1:8188`.

## What was inspected

Full `/object_info` inventory (1050 node classes) filtered for context,
continuation, extend, previous/latent and long-video nodes.

| Candidate node | Inputs | Why it is not eligible |
|---|---|---|
| `ContextWindowsManual` | model, context_length, context_overlap, context_schedule, … | A model-level windowing patch for long-video sampling. It does **not** consume a predecessor's decoded video or latent artifact, and it does **not** accept the supplied last image. |
| `Flux3VideoContinuationNode` | video, prompt, duration, resolution, … | Continuation from a prior video, but **no first/last image conditioning**. Contract §15.3: continuation alone does not satisfy last-image conditioning. |
| `GrokVideoExtendNode` | prompt, video, model, seed | Same limitation: prior video only, no terminal image. |
| `WanFirstLastFrameToVideo` | positive, negative, vae, width, height, length, batch_size | Terminal-image conditioning, but **no predecessor context** input. |
| `WanImageToVideo` | positive, negative, vae, width, height, length, batch_size | First-image only, no predecessor context. |

No installed node exports or imports a reusable Wan temporal latent, and no node
pairs a predecessor artifact with the supplied last image.

## Missing component

A verified ComfyUI graph (and supporting custom node/model) that accepts **both**:

1. a predecessor artifact — a decoded video tail (tail-frame context) or a
   validated temporal latent (native latent); and
2. the segment's supplied **last image** as an endpoint.

## Required setup to unblock

- Author/import a ComfyUI API graph that takes a predecessor frames/latent input
  **and** a last-image input, export it, and import it into vid-build with typed
  bindings: `context_video_asset` / `context_latent_asset`, `context_frame_count`,
  `last_frame_asset`, plus the existing `first_frame_asset` and movement bindings.
- Declare `capabilities.inputs.reference_image` / `continuation.mode = tail_frames
  | native_latent` and `inputs.end_image = true` on that workflow version.
- Run it once against a two-segment example and record the artifact contract
  (frame ranges, layout for latents) before marking context available.

## App-side readiness (already implemented)

- `motion_contexts` schema and `anchor_only` scheduling/invalidation exist.
- Preflight rejects context modes with an actionable message today.
- Tail-frame/latent scheduling (sequential consumers, transitive invalidation)
  is gated on a verified adapter; it is **not** claimed as implemented.

## Honest statement

The context dropdown stays disabled and the plan rejects non-anchor modes. A
schema column is not an adapter; this report does not claim otherwise.