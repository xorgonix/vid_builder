package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"vidbuild/internal/continuity"
	"vidbuild/internal/templates"
)

func (s *Server) stateURL(r *http.Request, shotID string) string {
	return s.videoBase(r) + "/shots/" + shotID + "/state"
}

// loadStateJSON returns the current state JSON for an endpoint, or "{}".
func (s *Server) loadStateJSON(r *http.Request, shotID, endpoint string) string {
	st, err := s.store.GetCurrentShotState(r.Context(), shotID, endpoint)
	if err != nil || st == nil || strings.TrimSpace(st.StateJSON) == "" {
		return "{}"
	}
	return st.StateJSON
}

func (s *Server) handleShotState(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "board"
	shot, err := s.store.GetShot(r.Context(), chiURLParam(r, "shotID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shot.ID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_, next, _ := s.store.NeighbourShots(r.Context(), shot.ID, video.ID)
	handoff := continuity.Handoff{}
	if next != nil {
		if link, err := s.store.GetShotLink(r.Context(), shot.ID, next.ID); err == nil {
			_ = json.Unmarshal([]byte(link.MetadataJSON), &handoff)
		}
	}
	data := templates.ContinuityData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Shot:         templates.ShotNode{ID: shot.ID, Title: shot.Title, Purpose: shot.Purpose, Treatment: shot.Treatment},
		StartState:   s.loadStateJSON(r, shot.ID, "start"),
		EndState:     s.loadStateJSON(r, shot.ID, "end"),
		Handoff:      handoff,
		Transitions:  continuity.TransitionTypes,
		InheritFlags: inheritFlagViews(handoff.Inherit),
	}
	if next != nil {
		data.NextShotID = next.ID
		data.NextShotTitle = next.Title
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.ContinuityPage(data))
}

func (s *Server) handleSaveShotState(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	endpoint := chiURLParam(r, "endpoint")
	if endpoint != "start" && endpoint != "end" {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	stateJSON := strings.TrimSpace(r.FormValue("state_json"))
	if stateJSON == "" {
		stateJSON = "{}"
	}
	if err := continuity.ValidateStateJSON(stateJSON); err != nil {
		http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	user := UserFrom(r.Context())
	if _, err := s.store.SaveShotState(r.Context(), shotID, endpoint, stateJSON, "human", user.ID); err != nil {
		s.log.Error("save shot state", "err", err)
		http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg("Could not save state.")+"&kind=error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg("State saved as a new version.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleTransitionOut(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.stateURL(r, chiURLParam(r, "shotID")), http.StatusSeeOther)
}

func (s *Server) handleSaveTransitionOut(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_, next, _ := s.store.NeighbourShots(r.Context(), shotID, video.ID)
	if next == nil {
		http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg("This shot has no following shot to transition into.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	handoff := continuity.Handoff{
		TransitionType: r.FormValue("transition_type"),
		DurationMs:     parseInt(r.FormValue("duration_ms"), 0),
		Motivation:     strings.TrimSpace(r.FormValue("motivation")),
		Notes:          strings.TrimSpace(r.FormValue("notes")),
		Inherit: continuity.InheritFlags{
			CharacterIdentity: r.FormValue("inherit_character_identity") != "",
			Costume:           r.FormValue("inherit_costume") != "",
			PhysicalCondition: r.FormValue("inherit_physical_condition") != "",
			EmotionalState:    r.FormValue("inherit_emotional_state") != "",
			KnowledgeState:    r.FormValue("inherit_knowledge_state") != "",
			PropState:         r.FormValue("inherit_prop_state") != "",
			PhysicalPositions: r.FormValue("inherit_physical_positions") != "",
			Location:          r.FormValue("inherit_location") != "",
			Lighting:          r.FormValue("inherit_lighting") != "",
			Weather:           r.FormValue("inherit_weather") != "",
		},
		Recompose: continuity.Recompose{
			Allowed:    r.FormValue("recompose_allowed") != "",
			ShotSize:   r.FormValue("recompose_shot_size"),
			Angle:      r.FormValue("recompose_angle"),
			CameraSide: r.FormValue("recompose_camera_side"),
		},
	}
	if err := handoff.Validate(); err != nil {
		http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	user := UserFrom(r.Context())
	if _, err := s.store.UpsertShotLink(r.Context(), shotID, next.ID, "continues_into", handoff.Marshal()); err != nil {
		s.log.Error("save transition", "err", err)
		http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg("Could not save transition.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "transition.save", "shot", shotID, "saved transition into next shot", "{}")
	http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg("Transition saved.")+"&kind=success", http.StatusSeeOther)
}

// handleDeriveNext proposes the next shot's start state from this shot's end
// state and the transition handoff. It never writes the neighbour directly
// (spec §6.5, §2.6).
func (s *Server) handleDeriveNext(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_, next, _ := s.store.NeighbourShots(r.Context(), shotID, video.ID)
	if next == nil {
		http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg("This shot has no following shot.")+"&kind=error", http.StatusSeeOther)
		return
	}
	handoff := continuity.Handoff{}
	if link, err := s.store.GetShotLink(r.Context(), shotID, next.ID); err == nil {
		_ = json.Unmarshal([]byte(link.MetadataJSON), &handoff)
	}
	if handoff.TransitionType == "" {
		http.Redirect(w, r, s.stateURL(r, shotID)+"?msg="+urlMsg("Set a transition before deriving the next state.")+"&kind=error", http.StatusSeeOther)
		return
	}
	var endState continuity.State
	if raw := s.loadStateJSON(r, shotID, "end"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &endState)
	}
	derivation := continuity.DeriveNextStart(endState, handoff, next.Purpose, s.bibleInvariants(r, project.ID))
	raw, _ := json.Marshal(struct {
		FromShotID string                `json:"from_shot_id"`
		ToShotID   string                `json:"to_shot_id"`
		Derivation continuity.Derivation `json:"derivation"`
	}{shotID, next.ID, derivation})
	user := UserFrom(r.Context())
	p, err := s.store.CreateProposal(r.Context(), project.ID, video.ID, "continuity", string(raw),
		"carry state into "+next.Title, s.cfg.LLM.Model, user.ID, next.Revision)
	if err != nil {
		s.log.Error("create continuity proposal", "err", err)
		s.notFound(w, r)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "proposal.create", "proposal", p.ID, "continuity proposal", "{}")
	http.Redirect(w, r, s.proposalURL(r, p.ID), http.StatusSeeOther)
}

// bibleInvariants collects project bible continuity notes for derivation.
func (s *Server) bibleInvariants(r *http.Request, projectID string) []string {
	var out []string
	if chars, err := s.store.ListCharacters(r.Context(), projectID); err == nil {
		for _, c := range chars {
			if strings.TrimSpace(c.Continuity) != "" && c.Continuity != "{}" {
				out = append(out, c.Name+": "+c.Continuity)
			}
		}
	}
	if locs, err := s.store.ListLocations(r.Context(), projectID); err == nil {
		for _, l := range locs {
			if strings.TrimSpace(l.Continuity) != "" && l.Continuity != "{}" {
				out = append(out, l.Name+": "+l.Continuity)
			}
		}
	}
	return out
}

func inheritFlagViews(f continuity.InheritFlags) []templates.InheritFlag {
	return []templates.InheritFlag{
		{Key: "character_identity", Label: "Character identity", Checked: f.CharacterIdentity},
		{Key: "costume", Label: "Costume", Checked: f.Costume},
		{Key: "physical_condition", Label: "Physical condition", Checked: f.PhysicalCondition},
		{Key: "emotional_state", Label: "Emotional state", Checked: f.EmotionalState},
		{Key: "knowledge_state", Label: "Knowledge state", Checked: f.KnowledgeState},
		{Key: "prop_state", Label: "Prop state", Checked: f.PropState},
		{Key: "physical_positions", Label: "Physical positions", Checked: f.PhysicalPositions},
		{Key: "location", Label: "Location", Checked: f.Location},
		{Key: "lighting", Label: "Lighting", Checked: f.Lighting},
		{Key: "weather", Label: "Weather", Checked: f.Weather},
	}
}
