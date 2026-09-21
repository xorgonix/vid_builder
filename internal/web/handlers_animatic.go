package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"vidbuild/internal/animatic"
	"vidbuild/internal/blocking"
	"vidbuild/internal/continuity"
	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

// buildPlaybackDocument assembles the bounded, authorized animatic payload.
func (s *Server) buildPlaybackDocument(r *http.Request, project *store.Project, video *store.Video) (*animatic.PlaybackDocument, error) {
	a, err := s.store.EnsureAnimatic(r.Context(), video.ID, actorID(r))
	if err != nil {
		return nil, err
	}
	doc := &animatic.PlaybackDocument{
		VideoID: video.ID, Title: video.Title, TargetDurationMs: video.TargetDurationMs,
		Status: a.Status, Revision: a.Revision, Settings: animatic.Parse(a.SettingsJSON),
	}
	ordered, err := s.store.ListVideoShotsOrdered(r.Context(), video.ID)
	if err != nil {
		return nil, err
	}
	metrics := s.metricsByEntity(r, video.ID)
	sceneTitles := map[string]string{}
	for i, sh := range ordered {
		ps := animatic.PlaybackShot{
			ID: sh.ID, Title: sh.Title, Purpose: sh.Purpose, DurationMs: sh.DurationTargetMs, Revision: sh.Revision,
			StartBlocking: s.loadBlockingDoc(r, sh.ID, "start_frame", "start"),
			EndBlocking:   s.loadBlockingDoc(r, sh.ID, "end_frame", "end"),
		}
		if sh.BeatID != "" {
			ps.BeatID = sh.BeatID
			if b, err := s.store.GetBeat(r.Context(), sh.BeatID); err == nil {
				ps.BeatTitle = b.Title
				ps.SceneID = b.SceneID
			}
		}
		if ps.SceneID != "" {
			if title, ok := sceneTitles[ps.SceneID]; ok {
				ps.SceneTitle = title
			} else if sc, err := s.store.GetScene(r.Context(), ps.SceneID); err == nil {
				ps.SceneTitle = sc.Title
				sceneTitles[ps.SceneID] = sc.Title
			}
		}
		if i < len(ordered)-1 {
			if link, err := s.store.GetShotLink(r.Context(), sh.ID, ordered[i+1].ID); err == nil {
				var h continuity.Handoff
				_ = json.Unmarshal([]byte(link.MetadataJSON), &h)
				ps.TransitionOut = animatic.Transition{Type: h.TransitionType, DurationMs: h.DurationMs}
			}
		}
		if cues, err := s.store.ListCues(r.Context(), sh.ID); err == nil {
			for _, c := range cues {
				pc := animatic.PlaybackCue{ID: c.ID, CueType: c.CueType, StartMs: c.StartMs, DurationMs: c.DurationMs,
					Content: c.Content, Delivery: c.Delivery, Subtext: c.Subtext}
				if c.CharacterID.Valid {
					pc.CharacterID = c.CharacterID.String
				}
				ps.Cues = append(ps.Cues, pc)
			}
		}
		if m, ok := metrics[sh.ID]; ok {
			ps.Metrics = m
		}
		doc.TotalDurationMs += sh.DurationTargetMs
		doc.Shots = append(doc.Shots, ps)
	}
	return doc, nil
}

// loadBlockingDoc returns the stored blocking for an endpoint, or a default.
func (s *Server) loadBlockingDoc(r *http.Request, shotID, specKind, role string) blocking.Spec {
	if b, err := s.store.GetBlockingForShot(r.Context(), shotID, specKind, role); err == nil && b != nil {
		return blocking.Parse(b.SpecJSON)
	}
	return blocking.Default()
}

func (s *Server) metricsByEntity(r *http.Request, videoID string) map[string]map[string]int {
	out := map[string]map[string]int{}
	metrics, err := s.store.ListCurrentMetricsForVideo(r.Context(), videoID)
	if err != nil {
		return out
	}
	for _, m := range metrics {
		vals := map[string]int{}
		if m.DramaticIntensity.Valid {
			vals["dramatic_intensity"] = int(m.DramaticIntensity.Int64)
		}
		if m.VisualIntensity.Valid {
			vals["visual_intensity"] = int(m.VisualIntensity.Int64)
		}
		if m.Tension.Valid {
			vals["tension"] = int(m.Tension.Int64)
		}
		if m.InformationDensity.Valid {
			vals["information_density"] = int(m.InformationDensity.Int64)
		}
		if m.EmotionalValence.Valid {
			vals["emotional_valence"] = int(m.EmotionalValence.Int64)
		}
		out[m.EntityID] = vals
	}
	return out
}

func (s *Server) handleAnimatic(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "animatic"
	doc, err := s.buildPlaybackDocument(r, project, video)
	if err != nil {
		s.log.Error("animatic", "err", err)
		s.notFound(w, r)
		return
	}
	data := templates.AnimaticData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Settings: doc.Settings, Status: doc.Status, Revision: doc.Revision, TotalDurationMs: doc.TotalDurationMs,
	}
	ordered, _ := s.store.ListVideoShotsOrdered(r.Context(), video.ID)
	metrics := s.metricsByEntity(r, video.ID)
	for _, sh := range ordered {
		view := templates.AnimaticShotView{
			ID: sh.ID, Title: sh.Title, Purpose: sh.Purpose, DurationMs: sh.DurationTargetMs, Revision: sh.Revision,
		}
		if sh.BeatID != "" {
			if b, err := s.store.GetBeat(r.Context(), sh.BeatID); err == nil {
				view.BeatTitle = b.Title
				if sc, err := s.store.GetScene(r.Context(), b.SceneID); err == nil {
					view.SceneTitle = sc.Title
				}
			}
		}
		view.StartSVG = blocking.RenderSVG(s.loadBlockingDoc(r, sh.ID, "start_frame", "start"))
		if cues, err := s.store.ListCues(r.Context(), sh.ID); err == nil {
			for _, c := range cues {
				cv := templates.CueView{ID: c.ID, CueType: c.CueType, Content: c.Content, Delivery: c.Delivery,
					Subtext: c.Subtext, StartMs: c.StartMs, DurationMs: c.DurationMs, Revision: c.Revision}
				if c.CharacterID.Valid {
					cv.CharacterID = c.CharacterID.String
				}
				view.Cues = append(view.Cues, cv)
			}
		}
		if m, ok := metrics[sh.ID]; ok {
			view.Metrics = templates.MetricValuesView{
				Dramatic: metricStr(m["dramatic_intensity"]), Visual: metricStr(m["visual_intensity"]),
				Tension: metricStr(m["tension"]), Info: metricStr(m["information_density"]),
				Valence: metricStr(m["emotional_valence"]),
			}
		}
		data.Shots = append(data.Shots, view)
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.AnimaticPage(data))
}

func (s *Server) handleAnimaticData(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	doc, err := s.buildPlaybackDocument(r, project, video)
	if err != nil {
		s.log.Error("animatic data", "err", err)
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(doc)
}

func (s *Server) handleAnimaticSettings(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	settings := animatic.Settings{
		Speed:             parseFloatOr(r.FormValue("speed"), 1),
		Captions:          r.FormValue("captions") != "",
		Voice:             r.FormValue("voice") != "",
		DisplayMode:       formValueOr(r, "display_mode", "blocking_only"),
		LoopMode:          formValueOr(r, "loop_mode", "none"),
		TransitionPreview: r.FormValue("transition_preview") != "",
		MetricsOverlay:    r.FormValue("metrics_overlay") != "",
	}
	if err := settings.Validate(); err != nil {
		http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	if _, err := s.store.EnsureAnimatic(r.Context(), video.ID, actorID(r)); err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.store.UpdateAnimaticSettings(r.Context(), video.ID, settings.Marshal(), parseInt(r.FormValue("revision"), 1)); err != nil {
		http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Animatic changed elsewhere; reloaded.")+"&kind=warning", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, actorID(r), "animatic.settings", "video", video.ID, "updated animatic settings", "{}")
	http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Playback settings saved.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleAnimaticApprove(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	if _, err := s.store.EnsureAnimatic(r.Context(), video.ID, actorID(r)); err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.store.SetAnimaticStatus(r.Context(), video.ID, "approved"); err != nil {
		s.log.Error("approve animatic", "err", err)
		http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Could not approve animatic.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, actorID(r), "animatic.approve", "video", video.ID, "approved animatic", "{}")
	http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Animatic approved.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleSetMetric(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	entityType := r.FormValue("entity_type")
	entityID := r.FormValue("entity_id")
	if !s.entityInVideo(r, entityType, entityID, video.ID) {
		s.notFound(w, r)
		return
	}
	values := store.MetricValues{
		DramaticIntensity:  optInt(r.FormValue("dramatic_intensity"), 0, 100),
		VisualIntensity:    optInt(r.FormValue("visual_intensity"), 0, 100),
		Tension:            optInt(r.FormValue("tension"), 0, 100),
		InformationDensity: optInt(r.FormValue("information_density"), 0, 100),
		EmotionalValence:   optInt(r.FormValue("emotional_valence"), -100, 100),
	}
	if _, err := s.store.SetMetric(r.Context(), video.ID, entityType, entityID, values, strings.TrimSpace(r.FormValue("notes")), actorID(r)); err != nil {
		http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, actorID(r), "metric.set", entityType, entityID, "set narrative metrics", "{}")
	http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Metrics saved.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) entityInVideo(r *http.Request, entityType, entityID, videoID string) bool {
	switch entityType {
	case "shot":
		ok, _ := s.store.ShotInVideo(r.Context(), entityID, videoID)
		return ok
	case "beat":
		ok, _ := s.store.BeatInVideo(r.Context(), entityID, videoID)
		return ok
	case "scene":
		ok, _ := s.store.SceneInVideo(r.Context(), entityID, videoID)
		return ok
	default:
		return false
	}
}

func metricStr(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func optInt(s string, lo, hi int) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return nil
	}
	return &n
}

func parseFloatOr(s string, def float64) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return def
	}
	return f
}
