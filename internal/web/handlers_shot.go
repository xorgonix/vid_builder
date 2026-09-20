package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"vidbuild/internal/templates"
)

var specKinds = map[string]string{
	"start_frame": "Start",
	"end_frame":   "End",
	"motion":      "Motion",
}

func normalizeSpecKind(k string) string {
	switch k {
	case "start_frame", "end_frame", "motion":
		return k
	default:
		return ""
	}
}

func (s *Server) handleShot(w http.ResponseWriter, r *http.Request) {
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
	inVideo, _ := s.store.ShotInVideo(r.Context(), shot.ID, video.ID)
	if !inVideo {
		s.notFound(w, r)
		return
	}
	data := templates.ShotDetailData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Shot: templates.ShotNode{ID: shot.ID, Title: shot.Title, Purpose: shot.Purpose, Treatment: shot.Treatment,
			Position: shot.Position, Revision: shot.Revision, DurationMs: shot.DurationTargetMs, Status: shot.Status, Readiness: shot.Readiness},
	}
	for _, kind := range []string{"start_frame", "end_frame", "motion"} {
		spec, err := s.store.GetCurrentSpec(r.Context(), shot.ID, kind)
		sv := templates.SpecView{Kind: kind, Label: specKinds[kind]}
		if err == nil && spec != nil {
			sv.Positive = spec.PositivePrompt
			sv.Negative = spec.NegativePrompt
			sv.Structured = spec.StructuredSpecJSON
			sv.Version = spec.Version
		}
		data.Specs = append(data.Specs, sv)
	}
	data.StartBlock = s.blockingView(r, shot.ID, "start_frame", "start")
	data.EndBlock = s.blockingView(r, shot.ID, "end_frame", "end")
	data.History = s.shotHistory(r, shot.ID)
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.ShotPage(data))
}

func (s *Server) blockingView(r *http.Request, shotID, specKind, role string) templates.BlockingView {
	b, err := s.store.GetBlockingForShot(r.Context(), shotID, specKind, role)
	if err != nil || b == nil {
		return templates.BlockingView{Role: role}
	}
	return templates.BlockingView{Role: role, SpecJSON: b.SpecJSON, Source: b.Source, Version: b.Version, Exists: true}
}

func (s *Server) shotHistory(r *http.Request, shotID string) []templates.HistoryItem {
	specs, _ := s.store.ListSpecs(r.Context(), shotID)
	var out []templates.HistoryItem
	for _, sp := range specs {
		out = append(out, templates.HistoryItem{When: sp.CreatedAt, Action: "spec " + sp.Kind, Detail: "v" + itoa(sp.Version)})
	}
	return out
}

func (s *Server) handleUpdateSpec(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	kind := normalizeSpecKind(chiURLParam(r, "kind"))
	if kind == "" {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	structured := strings.TrimSpace(r.FormValue("structured"))
	if structured == "" {
		structured = "{}"
	}
	if !json.Valid([]byte(structured)) {
		http.Redirect(w, r, s.shotURL(r, shotID)+"?msg="+urlMsg("Structured spec must be valid JSON.")+"&kind=error", http.StatusSeeOther)
		return
	}
	user := UserFrom(r.Context())
	if _, _, err := s.store.UpdateSpec(r.Context(), shotID, user.ID, kind,
		strings.TrimSpace(r.FormValue("positive")), strings.TrimSpace(r.FormValue("negative")), structured, "{}"); err != nil {
		s.log.Error("update spec", "err", err)
		http.Redirect(w, r, s.shotURL(r, shotID)+"?msg="+urlMsg("Could not save specification.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_, _ = s.store.RecomputeShotReadiness(r.Context(), shotID)
	http.Redirect(w, r, s.shotURL(r, shotID)+"?msg="+urlMsg(specKinds[kind]+" specification saved as a new version.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleSuggestSpec(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	kind := normalizeSpecKind(chiURLParam(r, "kind"))
	if kind == "" {
		s.notFound(w, r)
		return
	}
	shot, err := s.store.GetShot(r.Context(), shotID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	current := ""
	if spec, err := s.store.GetCurrentSpec(r.Context(), shotID, kind); err == nil && spec != nil {
		current = spec.PositivePrompt + "\n" + spec.NegativePrompt
	}
	suggestion, err := s.planner.SuggestSpec(r.Context(), s.plannerContext(r, project, video), specKinds[kind], shot.Purpose, shot.Treatment, current)
	if err != nil {
		s.log.Error("suggest spec", "err", err)
		http.Redirect(w, r, s.shotURL(r, shotID)+"?msg="+urlMsg("Suggestion failed: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	payload := map[string]any{
		"shot_id":  shotID,
		"kind":     kind,
		"positive": suggestion.Positive,
		"negative": suggestion.Negative,
	}
	structuredBytes, _ := json.Marshal(suggestion.Structured)
	payload["structured"] = string(structuredBytes)
	raw, _ := json.Marshal(payload)
	user := UserFrom(r.Context())
	p, err := s.store.CreateProposal(r.Context(), project.ID, video.ID, "prompt_revision", string(raw), suggestion.Summary, s.cfg.LLM.Model, user.ID, shot.Revision)
	if err != nil {
		s.log.Error("create spec proposal", "err", err)
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, s.proposalURL(r, p.ID), http.StatusSeeOther)
}

func (s *Server) shotURL(r *http.Request, shotID string) string {
	return s.videoBase(r) + "/shots/" + shotID
}

// handleShotHistory renders the shot inspector, whose History tab lists
// specification versions, proposals, attempts and decisions.
func (s *Server) handleShotHistory(w http.ResponseWriter, r *http.Request) {
	s.handleShot(w, r)
}
