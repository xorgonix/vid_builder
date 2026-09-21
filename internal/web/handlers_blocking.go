package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"vidbuild/internal/blocking"
	"vidbuild/internal/templates"
)

// validateBlockingScope rejects references to entities outside the project
// (spec §13.3): every actor character_id must belong to this project.
func (s *Server) validateBlockingScope(r *http.Request, projectID, specJSON string) error {
	spec := blocking.Parse(specJSON)
	need := map[string]bool{}
	for _, a := range spec.Actors {
		if a.CharacterID != "" {
			need[a.CharacterID] = true
		}
	}
	if len(need) == 0 {
		return nil
	}
	chars, err := s.store.ListCharacters(r.Context(), projectID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, c := range chars {
		have[c.ID] = true
	}
	for id := range need {
		if !have[id] {
			return fmt.Errorf("blocking: character %s does not belong to this project", id)
		}
	}
	return nil
}

func roleSpecKind(role string) string {
	switch role {
	case "start":
		return "start_frame"
	case "end":
		return "end_frame"
	default:
		return ""
	}
}

func (s *Server) handleBlockingEditor(w http.ResponseWriter, r *http.Request) {
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
	start := s.loadBlockingSpec(r, shot.ID, "start_frame", "start")
	end := s.loadBlockingSpec(r, shot.ID, "end_frame", "end")
	data := templates.BlockingEditorData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Shot:  templates.ShotNode{ID: shot.ID, Title: shot.Title, Purpose: shot.Purpose, Treatment: shot.Treatment, Revision: shot.Revision, DurationMs: shot.DurationTargetMs, Status: shot.Status, Readiness: shot.Readiness},
		Start: start, End: end,
		StartSVG: blocking.RenderSVG(blocking.Parse(start.SpecJSON)),
		EndSVG:   blocking.RenderSVG(blocking.Parse(end.SpecJSON)),
		Analysis: blocking.Analyze(blocking.Parse(start.SpecJSON), blocking.Parse(end.SpecJSON)),
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.BlockingPage(data))
}

func (s *Server) loadBlockingSpec(r *http.Request, shotID, specKind, role string) templates.BlockingView {
	b, err := s.store.GetBlockingForShot(r.Context(), shotID, specKind, role)
	if err != nil || b == nil {
		return templates.BlockingView{Role: role}
	}
	return templates.BlockingView{Role: role, SpecJSON: b.SpecJSON, Source: b.Source, Version: b.Version, Exists: true}
}

func (s *Server) handleSaveBlocking(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	role := chiURLParam(r, "role")
	specKind := roleSpecKind(role)
	if specKind == "" {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	specJSON := r.FormValue("spec_json")
	if err := blocking.ValidateJSON(specJSON); err != nil {
		http.Redirect(w, r, s.blockingURL(r, shotID)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	if err := s.validateBlockingScope(r, project.ID, specJSON); err != nil {
		http.Redirect(w, r, s.blockingURL(r, shotID)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	spec, err := s.store.GetCurrentSpec(r.Context(), shotID, specKind)
	if err != nil {
		s.notFound(w, r)
		return
	}
	user := UserFrom(r.Context())
	if _, err := s.store.SaveBlocking(r.Context(), spec.ID, role, specJSON, "human", "", user.ID); err != nil {
		s.log.Error("save blocking", "err", err)
		http.Redirect(w, r, s.blockingURL(r, shotID)+"?msg="+urlMsg("Could not save blocking.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_, _ = s.store.RecomputeShotReadiness(r.Context(), shotID)
	http.Redirect(w, r, s.blockingURL(r, shotID)+"?msg="+urlMsg("Blocking saved as a new version.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleCopyBlocking(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	targetRole := chiURLParam(r, "role")
	targetKind := roleSpecKind(targetRole)
	if targetKind == "" {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	sourceRole := "end"
	if targetRole == "end" {
		sourceRole = "start"
	}
	source, err := s.store.GetBlockingForShot(r.Context(), shotID, roleSpecKind(sourceRole), sourceRole)
	specJSON := blocking.Default().Marshal()
	if err == nil && source != nil {
		specJSON = source.SpecJSON
	}
	targetSpec, err := s.store.GetCurrentSpec(r.Context(), shotID, targetKind)
	if err != nil {
		s.notFound(w, r)
		return
	}
	user := UserFrom(r.Context())
	if _, err := s.store.SaveBlocking(r.Context(), targetSpec.ID, targetRole, specJSON, "human", "", user.ID); err != nil {
		s.log.Error("copy blocking", "err", err)
	}
	http.Redirect(w, r, s.blockingURL(r, shotID)+"?msg="+urlMsg("Copied "+sourceRole+" blocking as starting point.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleSuggestBlocking(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
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
	startPrompt, endPrompt := "", ""
	if sp, err := s.store.GetCurrentSpec(r.Context(), shotID, "start_frame"); err == nil && sp != nil {
		startPrompt = sp.PositivePrompt
	}
	if sp, err := s.store.GetCurrentSpec(r.Context(), shotID, "end_frame"); err == nil && sp != nil {
		endPrompt = sp.PositivePrompt
	}
	suggestion, err := s.planner.SuggestBlocking(r.Context(), s.plannerContext(r, project, video), shot.Purpose, shot.Treatment, startPrompt, endPrompt)
	if err != nil {
		s.log.Error("suggest blocking", "err", err)
		http.Redirect(w, r, s.blockingURL(r, shotID)+"?msg="+urlMsg("Blocking suggestion failed: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"shot_id": shotID,
		"start":   suggestion.Start,
		"end":     suggestion.End,
		"summary": suggestion.Summary,
	})
	user := UserFrom(r.Context())
	p, err := s.store.CreateProposal(r.Context(), project.ID, video.ID, "blocking", string(payload), suggestion.Summary, s.cfg.LLM.Model, user.ID, shot.Revision)
	if err != nil {
		s.log.Error("create blocking proposal", "err", err)
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, s.proposalURL(r, p.ID), http.StatusSeeOther)
}

func (s *Server) blockingURL(r *http.Request, shotID string) string {
	return s.videoBase(r) + "/shots/" + shotID + "/blocking"
}
