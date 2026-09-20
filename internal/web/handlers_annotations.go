package web

import (
	"net/http"
	"strings"

	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

func (s *Server) handleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Annotation text is required.")+"&kind=error", http.StatusSeeOther)
		return
	}
	shotID := r.FormValue("shot_id")
	assetID := r.FormValue("asset_id")
	// Scope checks: a referenced shot must belong to this video; an asset to this workspace.
	if shotID != "" {
		if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
			s.notFound(w, r)
			return
		}
	}
	if assetID != "" {
		asset, err := s.store.GetAsset(r.Context(), assetID)
		if err != nil || asset.WorkspaceID != project.WorkspaceID {
			s.notFound(w, r)
			return
		}
	}
	user := UserFrom(r.Context())
	if _, err := s.store.CreateAnnotation(r.Context(), project.WorkspaceID, project.ID, video.ID, shotID, assetID, body, user.ID); err != nil {
		s.log.Error("create annotation", "err", err)
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Could not add annotation.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.EmitDomainEvent(r.Context(), project.WorkspaceID, "video", video.ID, "annotation.created", "{}")
	http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Annotation added.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleUpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	id := chiURLParam(r, "annotationID")
	if _, err := s.store.GetAnnotation(r.Context(), ws.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	if status := r.FormValue("status"); status != "" {
		if err := s.store.SetAnnotationStatus(r.Context(), id, status); err != nil {
			s.log.Error("set annotation status", "err", err)
		}
	}
	if assigned := r.FormValue("assigned_to"); assigned != "" {
		_ = s.store.ReassignAnnotation(r.Context(), id, assigned)
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "annotation.update", "annotation", id, "updated annotation", "{}")
	http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Annotation updated.")+"&kind=success", http.StatusSeeOther)
}

// annotationViews maps stored annotations to view rows, resolving author names.
func (s *Server) annotationViews(r *http.Request, annotations []store.Annotation) []templates.AnnotationView {
	out := make([]templates.AnnotationView, 0, len(annotations))
	for _, a := range annotations {
		v := templates.AnnotationView{ID: a.ID, Body: a.Body, Status: a.Status, CreatedAt: a.CreatedAt}
		if u, err := s.store.GetUser(r.Context(), a.CreatedBy); err == nil {
			v.Author = u.DisplayName
		}
		if a.ShotID.Valid {
			v.ShotID = a.ShotID.String
			if shot, err := s.store.GetShot(r.Context(), a.ShotID.String); err == nil {
				v.ShotTitle = shot.Title
			}
		}
		if a.AssetID.Valid {
			v.AssetID = a.AssetID.String
		}
		out = append(out, v)
	}
	return out
}
