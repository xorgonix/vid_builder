package web

import (
	"net/http"
	"strings"

	"vidbuild/internal/store"
)

func (s *Server) animaticURL(r *http.Request) string { return s.videoBase(r) + "/animatic" }

func (s *Server) handleCreateCue(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := chiURLParam(r, "shotID")
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	cueType := r.FormValue("cue_type")
	_, err := s.store.CreateCue(r.Context(), shotID, cueType,
		strings.TrimSpace(r.FormValue("character_id")),
		strings.TrimSpace(r.FormValue("content")),
		strings.TrimSpace(r.FormValue("delivery")),
		strings.TrimSpace(r.FormValue("subtext")),
		parseInt(r.FormValue("start_ms"), 0),
		parseInt(r.FormValue("duration_ms"), 0))
	if err != nil {
		http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Cue added.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) cueInVideo(r *http.Request, cueID, videoID string) (*store.ShotCue, bool) {
	cue, err := s.store.GetCue(r.Context(), cueID)
	if err != nil {
		return nil, false
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), cue.ShotID, videoID); !inVideo {
		return nil, false
	}
	return cue, true
}

func (s *Server) handleUpdateCue(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	cue, ok := s.cueInVideo(r, chiURLParam(r, "cueID"), video.ID)
	if !ok {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	err := s.store.UpdateCue(r.Context(), cue.ID,
		strings.TrimSpace(r.FormValue("content")),
		strings.TrimSpace(r.FormValue("delivery")),
		strings.TrimSpace(r.FormValue("subtext")),
		parseInt(r.FormValue("start_ms"), 0),
		parseInt(r.FormValue("duration_ms"), 0),
		parseInt(r.FormValue("revision"), 1))
	if err != nil {
		http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Cue changed elsewhere or timing is invalid.")+"&kind=warning", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.animaticURL(r), http.StatusSeeOther)
}

func (s *Server) handleDeleteCue(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	cue, ok := s.cueInVideo(r, chiURLParam(r, "cueID"), video.ID)
	if !ok {
		s.notFound(w, r)
		return
	}
	if err := s.store.DeleteCue(r.Context(), cue.ID); err != nil {
		s.log.Error("delete cue", "err", err)
	}
	http.Redirect(w, r, s.animaticURL(r)+"?msg="+urlMsg("Cue removed.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleMoveCue(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	cue, ok := s.cueInVideo(r, chiURLParam(r, "cueID"), video.ID)
	if !ok {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	_ = s.store.MoveCue(r.Context(), cue.ID, parseInt(r.FormValue("direction"), 1))
	http.Redirect(w, r, s.animaticURL(r), http.StatusSeeOther)
}
