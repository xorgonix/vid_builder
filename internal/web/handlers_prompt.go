package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"vidbuild/internal/continuity"
	"vidbuild/internal/prompt"
	"vidbuild/internal/templates"
)

var layerScopes = []string{"project_style", "character", "location", "continuity", "first_frame", "last_frame", "motion", "workflow", "experiment"}

// continuityBase projects a shot's semantic endpoint state into prompt context
// so continuity feeds the compiled prompt (spec §5.7).
func (s *Server) continuityBase(r *http.Request, shotID, kind string) string {
	var parts []string
	load := func(endpoint string) {
		var st continuity.State
		if err := json.Unmarshal([]byte(s.loadStateJSON(r, shotID, endpoint)), &st); err != nil {
			return
		}
		for _, c := range st.Characters {
			if c.Name != "" {
				parts = append(parts, c.Name)
			}
		}
		for _, p := range st.Props {
			if p.Name != "" {
				parts = append(parts, p.Name)
			}
		}
		for _, v := range []string{st.Setting, st.Time, st.Weather, st.Emotion} {
			if v != "" {
				parts = append(parts, v)
			}
		}
	}
	switch kind {
	case "end_frame":
		load("end")
	case "motion":
		load("start")
		load("end")
	default:
		load("start")
	}
	return strings.Join(parts, ", ")
}

// promptBaseWithContinuity combines project style context with endpoint state.
func (s *Server) promptBaseWithContinuity(r *http.Request, projectID, shotID, kind string) (string, string) {
	basePos, baseNeg := s.promptBase(r, projectID)
	if cb := s.continuityBase(r, shotID, kind); cb != "" {
		if basePos == "" {
			basePos = cb
		} else {
			basePos = basePos + ", " + cb
		}
	}
	return basePos, baseNeg
}

func (s *Server) promptBase(r *http.Request, projectID string) (string, string) {
	var parts []string
	if brief, err := s.store.GetCurrentBrief(r.Context(), projectID); err == nil && brief != nil && brief.VisualStyle != "" {
		parts = append(parts, brief.VisualStyle)
	}
	if rules, err := s.store.ListStyleRules(r.Context(), projectID); err == nil {
		for _, sr := range rules {
			if sr.Text != "" {
				parts = append(parts, sr.Text)
			}
		}
	}
	return strings.Join(parts, ", "), ""
}

func (s *Server) loadLayers(r *http.Request, shotID, kind string) []prompt.Layer {
	raw, _ := s.store.GetPromptLayers(r.Context(), shotID, kind)
	var layers []prompt.Layer
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &layers)
	}
	return layers
}

func (s *Server) handlePromptWorkbench(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "board"
	shotID := chiURLParam(r, "shotID")
	shot, err := s.store.GetShot(r.Context(), shotID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shot.ID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	kind := normalizeSpecKind(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = "start_frame"
	}
	basePos, baseNeg := s.promptBaseWithContinuity(r, project.ID, shotID, kind)
	layers := s.loadLayers(r, shotID, kind)
	spec, _ := s.store.GetCurrentSpec(r.Context(), shotID, kind)
	data := templates.PromptWorkbenchData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Shot: templates.ShotNode{ID: shot.ID, Title: shot.Title, Purpose: shot.Purpose},
		Kind: kind, KindLabel: specKinds[kind],
		BasePositive: basePos, BaseNegative: baseNeg,
		Scopes: layerScopes,
		Modes:  []string{prompt.ModeInherit, prompt.ModeAppend, prompt.ModeReplace, prompt.ModeSuppress},
	}
	for _, l := range layers {
		l.Normalize()
		data.Layers = append(data.Layers, templates.LayerView{
			Scope: l.Scope, Mode: l.Mode, Positive: l.Positive, Negative: l.Negative,
			Locked: l.Locked, Priority: l.Priority,
		})
	}
	if spec != nil {
		data.Positive = spec.PositivePrompt
		data.Negative = spec.NegativePrompt
	}
	compiled := prompt.Compile(basePos, baseNeg, layers)
	data.CompiledPositive = compiled.Positive
	data.CompiledNegative = compiled.Negative
	data.Applied = compiled.Applied
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.PromptWorkbenchPage(data))
}

func layersFromForm(r *http.Request) []prompt.Layer {
	count := parseInt(r.FormValue("layers_count"), 0)
	var out []prompt.Layer
	for i := 0; i < count; i++ {
		scope := strings.TrimSpace(r.FormValue("scope_" + itoa(i)))
		mode := r.FormValue("mode_" + itoa(i))
		positive := strings.TrimSpace(r.FormValue("positive_" + itoa(i)))
		if positive == "" {
			positive = strings.TrimSpace(r.FormValue("text_" + itoa(i)))
		}
		negative := strings.TrimSpace(r.FormValue("negative_" + itoa(i)))
		if scope == "" && positive == "" && negative == "" {
			continue
		}
		l := prompt.Layer{
			Scope: scope, Mode: mode, Positive: positive, Negative: negative,
			Locked:   r.FormValue("locked_"+itoa(i)) != "",
			Priority: parseInt(r.FormValue("priority_"+itoa(i)), 0),
		}
		l.Normalize()
		out = append(out, l)
	}
	return out
}

func (s *Server) handlePromptPreview(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	kind := normalizeSpecKind(r.FormValue("kind"))
	if kind == "" {
		kind = "start_frame"
	}
	basePos, baseNeg := s.promptBaseWithContinuity(r, project.ID, shotID, kind)
	_ = kind
	layers := layersFromForm(r)
	compiled := prompt.Compile(basePos, baseNeg, layers)
	s.renderComponent(w, r, templates.CompiledPreviewFragment(compiled.Positive, compiled.Negative, compiled.Applied))
}

func (s *Server) handlePromptSave(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	kind := normalizeSpecKind(r.FormValue("kind"))
	if kind == "" {
		kind = "start_frame"
	}
	basePos, baseNeg := s.promptBaseWithContinuity(r, project.ID, shotID, kind)
	layers := layersFromForm(r)
	compiled := prompt.Compile(basePos, baseNeg, layers)
	layersJSON, _ := json.Marshal(layers)
	user := UserFrom(r.Context())
	if _, err := s.store.SaveSpecWithLayers(r.Context(), shotID, user.ID, kind, compiled.Positive, compiled.Negative, string(layersJSON)); err != nil {
		s.log.Error("save prompt layers", "err", err)
		http.Redirect(w, r, withQuery(s.promptURL(r, shotID, kind), flashQuery("Could not save prompt layers.", "error")), http.StatusSeeOther)
		return
	}
	_, _ = s.store.RecomputeShotReadiness(r.Context(), shotID)
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "prompt.save", "shot", shotID, "saved prompt layers as new spec version", "{}")
	http.Redirect(w, r, withQuery(s.promptURL(r, shotID, kind), flashQuery("Prompt layers saved as a new specification version.", "success")), http.StatusSeeOther)
}

func (s *Server) promptURL(r *http.Request, shotID, kind string) string {
	return s.videoBase(r) + "/shots/" + shotID + "/prompt-workbench?kind=" + kind
}
