package web

import (
	"errors"
	"net/http"
	"strings"

	"vidbuild/internal/store"
)

func (s *Server) handleCreateBeat(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	sceneID := chiURLParam(r, "sceneID")
	if inVideo, _ := s.store.SceneInVideo(r.Context(), sceneID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "New beat"
	}
	user := UserFrom(r.Context())
	beat, err := s.store.CreateBeat(r.Context(), sceneID, title,
		strings.TrimSpace(r.FormValue("purpose")),
		strings.TrimSpace(r.FormValue("trigger")),
		strings.TrimSpace(r.FormValue("action")),
		strings.TrimSpace(r.FormValue("reaction")),
		strings.TrimSpace(r.FormValue("resulting_state")),
		strings.TrimSpace(r.FormValue("audience_effect")),
		parseInt(r.FormValue("budget_s"), 0)*1000)
	if err != nil {
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "beat.create", "beat", beat.ID, "created beat", "{}")
	http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Beat added.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleUpdateBeat(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	id := chiURLParam(r, "beatID")
	if inVideo, _ := s.store.BeatInVideo(r.Context(), id, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	err := s.store.UpdateBeat(r.Context(), id,
		r.FormValue("title"),
		strings.TrimSpace(r.FormValue("purpose")),
		strings.TrimSpace(r.FormValue("trigger")),
		strings.TrimSpace(r.FormValue("action")),
		strings.TrimSpace(r.FormValue("reaction")),
		strings.TrimSpace(r.FormValue("resulting_state")),
		strings.TrimSpace(r.FormValue("audience_effect")),
		parseInt(r.FormValue("budget_s"), 0)*1000,
		parseInt(r.FormValue("revision"), 1))
	if err != nil {
		kind := "warning"
		msg := "Beat changed elsewhere; reloaded."
		if !errors.Is(err, store.ErrConflict) {
			kind = "error"
			msg = err.Error()
		}
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg(msg)+"&kind="+kind, http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "beat.update", "beat", id, "updated beat", "{}")
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleDeleteBeat(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	id := chiURLParam(r, "beatID")
	if inVideo, _ := s.store.BeatInVideo(r.Context(), id, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	user := UserFrom(r.Context())
	err := s.store.DeleteBeat(r.Context(), id)
	if err != nil {
		msg := "Could not delete beat."
		switch {
		case errors.Is(err, store.ErrBeatHasShots):
			msg = "Move or delete this beat's shots first; shots are never silently destroyed."
		case errors.Is(err, store.ErrLastBeat):
			msg = "A scene must keep at least one beat."
		}
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg(msg)+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "beat.delete", "beat", id, "deleted beat", "{}")
	http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Beat removed.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleMoveBeat(w http.ResponseWriter, r *http.Request) {
	if _, video, _, ok := s.loadVideoCtx(w, r); !ok {
		return
	} else if inVideo, _ := s.store.BeatInVideo(r.Context(), chiURLParam(r, "beatID"), video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	_ = s.store.MoveBeat(r.Context(), chiURLParam(r, "beatID"), parseInt(r.FormValue("direction"), 1))
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}
