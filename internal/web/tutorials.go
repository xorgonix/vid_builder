package web

import (
	"encoding/json"
	"net/http"

	"vidbuild/internal/templates"
)

// TutorialStep is one scripted action in a guided tour. The driver in
// /static/tutorial.js executes steps against the real interface.
type TutorialStep struct {
	Route     string `json:"route,omitempty"`
	Target    string `json:"target,omitempty"`
	Action    string `json:"action"`
	Value     string `json:"value,omitempty"`
	Caption   string `json:"caption"`
	Navigates bool   `json:"navigates,omitempty"`
}

// Tutorial is a scripted, software-driven walkthrough of real operations.
type Tutorial struct {
	Slug       string
	Title      string
	Summary    string
	StartRoute string // empty means "start wherever you are"
	VideoScope bool   // needs a project/video already open
	Steps      []TutorialStep
}

var tutorials = []Tutorial{
	{
		Slug:       "setup",
		Title:      "Create a workspace and project",
		Summary:    "Watch vid-build create a workspace, then a project with its first video. This runs real operations in a new “Tutorial Studio” workspace, so your existing work is untouched.",
		StartRoute: "/workspaces",
		Steps: []TutorialStep{
			{Action: "highlight", Target: "button[onclick*='create_ws']", Caption: "Every body of work lives in a workspace — your ownership and permission boundary. This button opens the new-workspace dialog."},
			{Action: "click", Target: "button[onclick*='create_ws']", Caption: "Opening the dialog…"},
			{Action: "fill", Target: "#create_ws input[name='name']", Value: "Tutorial Studio", Caption: "Name the workspace. A personal workspace is a good default."},
			{Action: "highlight", Target: "#create_ws button[type='submit']", Caption: "Creating it records you as the owner."},
			{Action: "click", Target: "#create_ws button[type='submit']", Navigates: true, Caption: "Submitting creates the workspace and opens its dashboard."},
			{Action: "highlight", Target: "a[href$='/projects/new']", Caption: "A project is a shared creative world: brief, references, characters and locations."},
			{Action: "click", Target: "a[href$='/projects/new']", Navigates: true, Caption: "Opening the new-project form…"},
			{Action: "fill", Target: "input[name='title']", Value: "Ceramic Studio Teaser", Caption: "Name the project."},
			{Action: "fill", Target: "input[name='video_title']", Value: "20s Teaser", Caption: "Name the first video inside it."},
			{Action: "fill", Target: "input[name='purpose']", Value: "show the tactile craft of a potter's studio in twenty seconds", Caption: "State the purpose in plain language — the studio keeps asking what a shot accomplishes."},
			{Action: "fill", Target: "input[name='duration_s']", Value: "20", Caption: "Set a target duration."},
			{Action: "highlight", Target: "form[action$='/projects'] button[type='submit']", Caption: "Create the project."},
			{Action: "click", Target: "form[action$='/projects'] button[type='submit']", Navigates: true, Caption: "Submitting creates the project and its first video."},
			{Action: "done", Caption: "You now have a workspace, a project and a video. Open the Outline tour next to shape the story."},
		},
	},
	{
		Slug:       "outline",
		Title:      "Shape the outline and read the board",
		Summary:    "Add a section, a scene and a shot, then see the shot appear on the Board. Run this while a project and video are open (the setup tour ends there).",
		VideoScope: true,
		Steps: []TutorialStep{
			{Action: "caption", Caption: "Make sure a project and a video are open, then press Next. The outline shapes the narrative before any visual detail."},
			{Action: "highlight", Target: "a[href$='/outline']", Caption: "Open the Outline from the left navigation."},
			{Action: "click", Target: "a[href$='/outline']", Navigates: true, Caption: "Opening the Outline…"},
			{Action: "highlight", Target: "button[onclick*='add_section']", Caption: "Sections group scenes and carry a purpose and a duration budget."},
			{Action: "click", Target: "button[onclick*='add_section']", Caption: "Opening the Add section dialog…"},
			{Action: "fill", Target: "#add_section input[name='title']", Value: "Craft", Caption: "Name the section."},
			{Action: "fill", Target: "#add_section textarea[name='purpose']", Value: "establish the material", Caption: "Say what this division accomplishes."},
			{Action: "click", Target: "#add_section button[type='submit']", Navigates: true, Caption: "Adding the section reloads the outline."},
			{Action: "highlight", Target: "input[placeholder='New scene title']", Caption: "Each scene holds beats, and each beat holds shots. This field adds a scene."},
			{Action: "caption", Caption: "Scenes carry a synopsis, purpose, pace and audience reaction. A shot is the smallest unit that begins in one state and ends in another."},
			{Action: "highlight", Target: "a[href$='/board']", Caption: "The Board shows every shot as START · CHANGE · END with its readiness."},
			{Action: "click", Target: "a[href$='/board']", Navigates: true, Caption: "Opening the Board…"},
			{Action: "highlight", Target: "main", Caption: "Blocking sketches render instantly as SVG — no model call, no cost. Open a shot to edit its blocking and compare the detected change."},
			{Action: "done", Caption: "That is the inexpensive creative loop: structure, see it, block it — all before spending a cent on pixels."},
		},
	},
	{
		Slug:       "duration-video",
		Title:      "Make a 5/15/30-second video",
		Summary:    "See how a prompt and a length become one playable video, built from fixed five-second anchor-to-anchor segments.",
		VideoScope: true,
		Steps: []TutorialStep{
			{Action: "caption", Caption: "Open a project and video, then press Next. The Duration video screen needs a shot whose start and end frames are selected and approved."},
			{Action: "highlight", Target: "a[href$='/duration']", Caption: "Open Duration video from the left navigation."},
			{Action: "click", Target: "a[href$='/duration']", Navigates: true, Caption: "Opening the Duration video screen…"},
			{Action: "highlight", Target: "select[name='shot_id']", Caption: "Pick a shot. It must already have an approved start and end frame — that is what anchors each segment."},
			{Action: "highlight", Target: "select[name='duration']", Caption: "Choose 5, 15 or 30 seconds. 5s is one generation; 15s is three; 30s is six — assembled in order."},
			{Action: "highlight", Target: "select[name='workflow_id']", Caption: "Choose a workflow that declares first/last frame bindings. There is no silent start-only fallback."},
			{Action: "fill", Target: "textarea[name='positive']", Value: "she shapes the clay, warm window light, tactile and calm", Caption: "Describe the motion. Each segment gets a distinct phrasing so adjacent clips are not identical."},
			{Action: "fill", Target: "textarea[name='negative']", Value: "lowres, blurry, distorted", Caption: "Say what to avoid."},
			{Action: "highlight", Target: "form[action$='/duration'] button[type='submit']", Caption: "Queue the video. One job runs per segment; if one fails you retry only that segment, and the final video appears here when every segment has succeeded."},
			{Action: "done", Caption: "Segments and the assembled video stay on this page: each segment has its own status, preview and Retry control."},
		},
	},
}

func tutorialBySlug(slug string) *Tutorial {
	for i := range tutorials {
		if tutorials[i].Slug == slug {
			return &tutorials[i]
		}
	}
	return nil
}

func (s *Server) handleTutorials(w http.ResponseWriter, r *http.Request) {
	shell := s.shell(r, "Guided tours")
	var items []templates.TutorialView
	for _, t := range tutorials {
		items = append(items, templates.TutorialView{
			Slug: t.Slug, Title: t.Title, Summary: t.Summary, StartRoute: t.StartRoute, VideoScope: t.VideoScope,
			Steps: len(t.Steps),
		})
	}
	s.renderPage(w, r, templates.TutorialsPage(templates.TutorialsData{Shell: shell, Tutorials: items}))
}

func (s *Server) handleTutorialScript(w http.ResponseWriter, r *http.Request) {
	t := tutorialBySlug(chiURLParam(r, "slug"))
	if t == nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"slug": t.Slug, "title": t.Title, "video_scope": t.VideoScope, "steps": t.Steps,
	})
}
