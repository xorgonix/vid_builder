package web

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"vidbuild/internal/store"
	"vidbuild/internal/templates"
	"vidbuild/internal/thumbs"
)

func thumbsIsImage(mime string) bool { return thumbs.IsImageMime(mime) }

// makeThumbnail derives and stores a thumbnail for one asset. Returns true on success.
func (s *Server) makeThumbnail(r *http.Request, a store.Asset) bool {
	f, err := s.media.Open(a.StorageURI)
	if err != nil {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<20))
	f.Close()
	if err != nil {
		return false
	}
	pngBytes, dw, dh, err := thumbs.GenerateImage(data, 0, 0)
	if err != nil {
		s.log.Warn("thumbnail: generate", "asset", a.ID, "err", err)
		return false
	}
	relDir := fmt.Sprintf("%s/%s/thumbs", a.WorkspaceID, a.ProjectID)
	uri, checksum, size, err := s.media.Put(relDir, a.ID+".png", bytes.NewReader(pngBytes))
	if err != nil {
		s.log.Warn("thumbnail: store", "asset", a.ID, "err", err)
		return false
	}
	if _, err := s.store.CreateThumbnail(r.Context(), a.WorkspaceID, a.ProjectID, a.ID, uri, "image/png", size, checksum, dw, dh); err != nil {
		s.log.Warn("thumbnail: record", "asset", a.ID, "err", err)
		return false
	}
	return true
}

func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "review"
	role := chiURLParam(r, "role")
	if role != "start_frame" && role != "end_frame" && role != "motion" {
		s.notFound(w, r)
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
	assets, _ := s.store.ListAssetsForShotRole(r.Context(), shot.ID, role)
	views := s.assetViews(r.Context(), assets)
	for i := range views {
		if ap, err := s.store.LatestApproval(r.Context(), views[i].ID); err == nil && ap != nil {
			views[i].Approval = ap.Decision
		}
	}
	data := templates.CandidatesData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Shot: templates.ShotNode{ID: shot.ID, Title: shot.Title, Purpose: shot.Purpose},
		Role: role, RoleLabel: roleLabel(role),
		Assets: views,
	}
	if sel, err := s.store.GetCurrentSelection(r.Context(), shot.ID, role); err == nil && sel != nil {
		data.SelectedID = sel.AssetID
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.CandidatesPage(data))
}

// handleBackfillThumbnails derives thumbnails for existing image assets.
func (s *Server) handleBackfillThumbnails(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	assets, err := s.store.ListAllAssets(r.Context())
	if err != nil {
		s.log.Error("list assets", "err", err)
	}
	made := 0
	for _, a := range assets {
		if a.WorkspaceID != ws.ID || a.Kind == "thumbnail" || !thumbsIsImage(a.MimeType) {
			continue
		}
		if s.store.HasThumbnail(r.Context(), a.ID) {
			continue
		}
		if s.makeThumbnail(r, a) {
			made++
		}
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "thumbnails.backfill", "workspace", ws.ID, "generated thumbnails", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/diagnostics?msg="+urlMsg("Generated "+itoa(made)+" thumbnail(s).")+"&kind=success", http.StatusSeeOther)
}
