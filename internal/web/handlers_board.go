package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"vidbuild/internal/blocking"
	"vidbuild/internal/continuity"
	"vidbuild/internal/templates"
)

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "board"
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "director"
	}
	filter := r.URL.Query().Get("readiness")
	data := templates.BoardData{Shell: shell, Project: toProjectView(project), Video: toVideoView(video), Mode: mode, Filter: filter}

	ordered, _ := s.store.ListVideoShotsOrdered(r.Context(), video.ID)
	nextByID := map[string]string{}
	for i, sh := range ordered {
		if i < len(ordered)-1 {
			nextByID[sh.ID] = ordered[i+1].ID
		}
	}

	sections, _ := s.store.ListSections(r.Context(), video.ID)
	for _, sec := range sections {
		scenes, _ := s.store.ListScenes(r.Context(), sec.ID)
		for _, sc := range scenes {
			shots, _ := s.store.ListShots(r.Context(), sc.ID)
			for _, sh := range shots {
				if sh.Status == "archived" {
					continue
				}
				if filter != "" && sh.Readiness != filter {
					continue
				}
				card := templates.BoardCard{
					ShotID: sh.ID, Title: sh.Title, Purpose: sh.Purpose, SceneTitle: sc.Title,
					Readiness: sh.Readiness, Status: sh.Status, DurationMs: sh.DurationTargetMs,
				}
				if b, err := s.store.GetBlockingForShot(r.Context(), sh.ID, "start_frame", "start"); err == nil && b != nil {
					card.StartSVG = blocking.RenderSVG(blocking.Parse(b.SpecJSON))
					card.HasStart = true
				}
				if b, err := s.store.GetBlockingForShot(r.Context(), sh.ID, "end_frame", "end"); err == nil && b != nil {
					card.EndSVG = blocking.RenderSVG(blocking.Parse(b.SpecJSON))
					card.HasEnd = true
				}
				// Endpoint precedence (spec §5.6): selected final → concept → blocking SVG.
				if url, ok := s.endpointAssetURL(r, sh.ID, "start_frame"); ok {
					card.StartAssetURL = url
					card.HasStartAsset = true
				}
				if url, ok := s.endpointAssetURL(r, sh.ID, "end_frame"); ok {
					card.EndAssetURL = url
					card.HasEndAsset = true
				}
				if sp, err := s.store.GetCurrentSpec(r.Context(), sh.ID, "motion"); err == nil && sp != nil {
					card.MotionText = sp.PositivePrompt
				}
				if mode == "continuity" {
					card.HasContinuity = true
					s.fillContinuity(r, &card, nextByID[sh.ID])
				}
				data.Cards = append(data.Cards, card)
			}
		}
	}
	data.Total = len(data.Cards)
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.BoardPage(data))
}

func (s *Server) handleBible(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	shell := s.shell(r, project.Title+" · Bible")
	shell.Project = &templates.EntityRef{ID: project.ID, Title: project.Title}
	shell.ActiveNav = "more"
	data := templates.BibleData{Shell: shell, Project: toProjectView(project)}
	characters, _ := s.store.ListCharacters(r.Context(), project.ID)
	for _, c := range characters {
		data.Characters = append(data.Characters, templates.CharacterView{ID: c.ID, Name: c.Name, Description: c.Description, Continuity: c.Continuity})
	}
	locations, _ := s.store.ListLocations(r.Context(), project.ID)
	for _, l := range locations {
		data.Locations = append(data.Locations, templates.LocationView{ID: l.ID, Name: l.Name, Description: l.Description, Continuity: l.Continuity})
	}
	rules, _ := s.store.ListStyleRules(r.Context(), project.ID)
	for _, sr := range rules {
		data.StyleRules = append(data.StyleRules, templates.StyleRuleView{ID: sr.ID, Category: sr.Category, Text: sr.Text, Priority: sr.Priority})
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.BiblePage(data))
}

func (s *Server) handleCreateCharacter(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	if _, err := s.store.CreateCharacter(r.Context(), project.ID, formValueOr(r, "name", "Unnamed"), r.FormValue("description"), "{}"); err != nil {
		s.log.Error("create character", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "character.create", "project", project.ID, "created character", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"/bible?msg="+urlMsg("Character added.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	if _, err := s.store.CreateLocation(r.Context(), project.ID, formValueOr(r, "name", "Unnamed"), r.FormValue("description"), "{}"); err != nil {
		s.log.Error("create location", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "location.create", "project", project.ID, "created location", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"/bible?msg="+urlMsg("Location added.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleCreateStyleRule(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	category := r.FormValue("category")
	if category == "" {
		category = "general"
	}
	if err := s.store.CreateStyleRule(r.Context(), project.ID, user.ID, category, r.FormValue("rule_text"), parseInt(r.FormValue("priority"), 0)); err != nil {
		s.log.Error("create style rule", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "style_rule.create", "project", project.ID, "created style rule", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"/bible?msg="+urlMsg("Style rule added.")+"&kind=success", http.StatusSeeOther)
}

// endpointAssetURL applies endpoint precedence: the selected final asset, then
// any generated concept/candidate asset, then nothing (blocking SVG is the
// fallback handled by the template) — spec §5.6.
func (s *Server) endpointAssetURL(r *http.Request, shotID, role string) (string, bool) {
	if sel, err := s.store.CurrentSelection(r.Context(), shotID, role); err == nil && sel != nil {
		return "/assets/" + sel.AssetID, true
	}
	if assets, err := s.store.ListAssetsForShotRole(r.Context(), shotID, role); err == nil && len(assets) > 0 {
		return "/assets/" + assets[0].ID, true
	}
	return "", false
}

// fillContinuity populates continuity-mode card fields: the transition into the
// next shot, what is inherited, screen direction and unexplained changes.
func (s *Server) fillContinuity(r *http.Request, card *templates.BoardCard, nextShotID string) {
	if nextShotID == "" {
		card.ContinuityWarnings = append(card.ContinuityWarnings, "Last shot: no outgoing transition")
	} else {
		card.NextShotID = nextShotID
		link, err := s.store.GetShotLink(r.Context(), card.ShotID, nextShotID)
		if err != nil {
			card.ContinuityWarnings = append(card.ContinuityWarnings, "No transition defined into the next shot")
		} else {
			var h continuity.Handoff
			_ = json.Unmarshal([]byte(link.MetadataJSON), &h)
			card.TransitionType = h.TransitionType
			card.TransitionMotivation = h.Motivation
			card.ScreenDirection = h.Recompose.CameraSide
			card.InheritedSummary = inheritSummary(h.Inherit)
			if h.TransitionType == "" {
				card.ContinuityWarnings = append(card.ContinuityWarnings, "Transition type is not set")
			}
			if strings.TrimSpace(h.Motivation) == "" {
				card.ContinuityWarnings = append(card.ContinuityWarnings, "Transition has no motivation")
			}
		}
	}
	if _, err := s.store.GetCurrentShotState(r.Context(), card.ShotID, "end"); err != nil {
		card.ContinuityWarnings = append(card.ContinuityWarnings, "No end state recorded")
	}
}

func inheritSummary(f continuity.InheritFlags) string {
	flags := []struct {
		label string
		on    bool
	}{
		{"identity", f.CharacterIdentity},
		{"costume", f.Costume},
		{"condition", f.PhysicalCondition},
		{"emotion", f.EmotionalState},
		{"knowledge", f.KnowledgeState},
		{"props", f.PropState},
		{"positions", f.PhysicalPositions},
		{"location", f.Location},
		{"lighting", f.Lighting},
		{"weather", f.Weather},
	}
	var parts []string
	for _, f := range flags {
		if f.on {
			parts = append(parts, f.label)
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

func formValueOr(r *http.Request, key, def string) string {
	if v := r.FormValue(key); v != "" {
		return v
	}
	return def
}

func (s *Server) handleMore(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "more"
	s.renderPage(w, r, templates.MorePage(templates.MoreData{Shell: shell, Project: toProjectView(project), Video: toVideoView(video)}))
}
