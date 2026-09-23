# vid-build 0.02 — Guided tours

*Watch the interface operate itself.*

vid-build ships **guided tours**: scripted walkthroughs where the software drives
the real interface — highlighting controls, filling fields and performing the
operations — while a caption explains each step. They are not videos and not
mockups; the tour runs against your live application, in your own session.

Open **Guided tours** from the left navigation (or the account menu) at
`/tutorials`.

---

## How a tour works

Each tour is a script of steps. A step can:

- **highlight** a control and explain it,
- **fill** a field with a value,
- **click** a button or link,
- pause on a **caption**,
- and **finish** with a summary.

Because a tour spans several pages, it remembers its position in your browser
session (`sessionStorage`). When a step navigates, the next page picks the tour
up where it left off.

### Controls

A small player sits at the bottom of the screen while a tour is running:

| Control | Effect |
|---|---|
| **Back** | Return to the previous step. |
| **Next** | Perform the current step and move on. |
| **Play / Pause** | Auto-advance through the steps (about two seconds each). |
| **Exit** | Stop the tour and clear its state. |

You can exit at any time. Closing the browser tab also ends the tour.

### What tours are allowed to do

- The **setup tour** creates its own **“Tutorial Studio”** workspace and a
  project and video inside it, so **your existing work is never touched**.
- Tours **never queue image or video generation**. They stop at the form and
  explain what would happen.
- Tours use your session and respect every permission and CSRF check the normal
  interface does — a tour cannot do anything you could not do by hand.

---

## The tours

### 1. Create a workspace and project

*Starts at `/workspaces`.*

The software opens the **New workspace** dialog, names it *Tutorial Studio*,
creates it, then opens the new-project form and fills in a project, a first
video, its purpose and a target duration before creating them. You end on the
new video page.

### 2. Shape the outline and read the board

*Run with a project and video open.*

The tour opens **Outline**, adds a **section**, points out the scene field, then
jumps to the **Board** and explains how blocking sketches render instantly as
SVG with no model call.

### 3. Make a Super Shot (2.0–30.0 seconds)

*Run with a project and video open.*

The tour opens **Super Shot**, then walks through choosing a shot with an
approved start and end image, choosing a duration (5, 15 and 30 are presets),
selecting a workflow with declared timing capability, and writing the movement
prompt. It explains that a short shot is one generation while longer shots become
an ordered chain of adjacent segments (A→B, B→C, C→D) whose intermediate images
and movement prompts you review before generation, that each segment retries
independently, and that the assembled video appears when every segment is
current.

---

## Running a tour from where you are

Video-scoped tours (2 and 3) need a project and video open. Start them from
`/tutorials` after opening a video, or open the video first and use the **Guided
tours** link. The first step of each tour tells you if anything needs to be open
before you continue.

If a step's control is not on the current page, the player says so and waits:
open the right screen, then press **Next**.

---

## Adding or changing tours

Tours are data, not code. The catalogue lives in
`internal/web/tutorials.go`; each step is a small record with an optional route,
a CSS selector target, an action (`caption`, `highlight`, `fill`, `click`,
`done`), a value and a caption. The driver is
`internal/web/static/tutorial.js`. Adding a tour is adding one entry to the
slice and re-running `templ generate` and the build.

---

*vid-build 0.02 — idea to finished film, one visible change at a time.*
