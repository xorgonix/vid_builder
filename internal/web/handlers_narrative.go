package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"vidbuild/internal/planner"
	"vidbuild/internal/store"
)

// handleProposeBeats asks the planner for the beats of one scene (spec §8.3).
func (s *Server) handleProposeBeats(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	sceneID := chiURLParam(r, "sceneID")
	scene, err := s.store.GetScene(r.Context(), sceneID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.SceneInVideo(r.Context(), sceneID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	payload, err := s.planner.BeatProposal(r.Context(), s.plannerContext(r, project, video), sceneContextText(scene), "", nil)
	if err != nil {
		s.log.Error("beat proposal", "err", err)
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Beat proposal failed: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	raw, _ := json.Marshal(struct {
		SceneID string `json:"scene_id"`
		*planner.BeatPayload
	}{sceneID, payload})
	user := UserFrom(r.Context())
	p, err := s.store.CreateProposal(r.Context(), project.ID, video.ID, "scene_plan", string(raw), payload.Summary, s.cfg.LLM.Model, user.ID, scene.Revision)
	if err != nil {
		s.log.Error("create beat proposal", "err", err)
		s.notFound(w, r)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "proposal.create", "proposal", p.ID, "beat proposal", "{}")
	http.Redirect(w, r, s.proposalURL(r, p.ID), http.StatusSeeOther)
}

// handleProposeShots asks the planner for the minimum shots of one beat (§8.4).
func (s *Server) handleProposeShots(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	beatID := chiURLParam(r, "beatID")
	beat, err := s.store.GetBeat(r.Context(), beatID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.BeatInVideo(r.Context(), beatID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	payload, err := s.planner.ShotBreakdownProposal(r.Context(), s.plannerContext(r, project, video), beatContextText(beat), "", nil)
	if err != nil {
		s.log.Error("shot breakdown proposal", "err", err)
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Shot proposal failed: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	raw, _ := json.Marshal(struct {
		BeatID string `json:"beat_id"`
		*planner.ShotBreakdownPayload
	}{beatID, payload})
	user := UserFrom(r.Context())
	p, err := s.store.CreateProposal(r.Context(), project.ID, video.ID, "shot_plan", string(raw), payload.Summary, s.cfg.LLM.Model, user.ID, beat.Revision)
	if err != nil {
		s.log.Error("create shot proposal", "err", err)
		s.notFound(w, r)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "proposal.create", "proposal", p.ID, "shot-breakdown proposal", "{}")
	http.Redirect(w, r, s.proposalURL(r, p.ID), http.StatusSeeOther)
}

// sceneContextText projects a scene into bounded planner context (spec §8.1).
func sceneContextText(sc *store.Scene) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", sc.Title)
	if sc.Purpose != "" {
		fmt.Fprintf(&b, "Purpose: %s\n", sc.Purpose)
	}
	if sc.Synopsis != "" {
		fmt.Fprintf(&b, "Synopsis: %s\n", sc.Synopsis)
	}
	if sc.Pace != "" {
		fmt.Fprintf(&b, "Pace: %s\n", sc.Pace)
	}
	if sc.AudienceReaction != "" {
		fmt.Fprintf(&b, "Intended reaction: %s\n", sc.AudienceReaction)
	}
	if sc.DurationBudgetMs > 0 {
		fmt.Fprintf(&b, "Duration budget: %d ms\n", sc.DurationBudgetMs)
	}
	return b.String()
}

// beatContextText projects a beat into bounded planner context.
func beatContextText(bt *store.Beat) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", bt.Title)
	for _, kv := range []struct{ label, value string }{
		{"Purpose", bt.Purpose},
		{"Trigger", bt.Trigger},
		{"Action", bt.Action},
		{"Reaction", bt.Reaction},
		{"Resulting state", bt.ResultingState},
		{"Intended audience effect", bt.IntendedAudienceEffect},
	} {
		if kv.value != "" {
			fmt.Fprintf(&b, "%s: %s\n", kv.label, kv.value)
		}
	}
	if bt.DurationBudgetMs > 0 {
		fmt.Fprintf(&b, "Duration budget: %d ms\n", bt.DurationBudgetMs)
	}
	return b.String()
}
