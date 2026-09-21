package web

import (
	"encoding/json"
	"net/http"

	"vidbuild/internal/templates"
)

// validEditTransition reports whether a transition kind is one the exporter can
// render (spec §13.3: validate transition documents).
func validEditTransition(v string) bool {
	switch v {
	case "cut", "fade", "dissolve":
		return true
	default:
		return false
	}
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "edit"
	user := UserFrom(r.Context())
	edit, err := s.store.PrimaryEdit(r.Context(), video.ID, user.ID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	data := templates.EditData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Edit: templates.EditView{ID: edit.ID, Name: edit.Name, Status: edit.Status, Revision: edit.Revision},
	}
	items, _ := s.store.ListEditItems(r.Context(), edit.ID)
	for _, it := range items {
		iv := templates.EditItemView{ID: it.ID, AssetID: it.AssetID, Position: it.Position, DurationMs: it.DurationMs, TransitionIn: it.TransitionIn, TransitionOut: it.TransitionOut, Revision: it.Revision, AssetURL: "/assets/" + it.AssetID}
		if it.ShotID.Valid {
			iv.ShotID = it.ShotID.String
			if shot, err := s.store.GetShot(r.Context(), it.ShotID.String); err == nil {
				iv.ShotTitle = shot.Title
			}
		}
		if asset, err := s.store.GetAsset(r.Context(), it.AssetID); err == nil {
			iv.Kind = asset.Kind
		}
		data.TotalMs += it.DurationMs
		data.Items = append(data.Items, iv)
	}

	// Available selected assets.
	sections, _ := s.store.ListSections(r.Context(), video.ID)
	for _, sec := range sections {
		scenes, _ := s.store.ListScenes(r.Context(), sec.ID)
		for _, sc := range scenes {
			shots, _ := s.store.ListShots(r.Context(), sc.ID)
			for _, sh := range shots {
				for _, role := range []string{"video", "start_frame", "end_frame"} {
					sel, err := s.store.GetCurrentSelection(r.Context(), sh.ID, role)
					if err != nil {
						continue
					}
					opt := templates.SelectionOption{
						ShotID: sh.ID, ShotTitle: sh.Title, Role: role, RoleLabel: roleLabel(role),
						AssetID: sel.AssetID, AssetURL: "/assets/" + sel.AssetID,
					}
					if thumb, err := s.store.GetThumbnailForAsset(r.Context(), sel.AssetID); err == nil && thumb != nil {
						opt.ThumbURL = "/assets/" + thumb.ID
					}
					data.Options = append(data.Options, opt)
				}
			}
		}
	}

	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.EditPage(data))
}

func (s *Server) handleAddEditItem(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	editID := chiURLParam(r, "editID")
	edit, err := s.store.GetEdit(r.Context(), video.ID, editID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	assetID := r.FormValue("asset_id")
	shotID := r.FormValue("shot_id")
	// The asset and shot must belong to this video's project (spec §13.3).
	asset, err := s.store.GetAsset(r.Context(), assetID)
	if err != nil || asset.WorkspaceID != WorkspaceFrom(r.Context()).ID || asset.ProjectID != video.ProjectID {
		http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("Unknown asset.")+"&kind=error", http.StatusSeeOther)
		return
	}
	if shotID != "" {
		if inVideo, _ := s.store.ShotInVideo(r.Context(), shotID, video.ID); !inVideo {
			http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("That shot is not in this video.")+"&kind=error", http.StatusSeeOther)
			return
		}
	}
	durationMs := parseInt(r.FormValue("duration_s"), 3) * 1000
	if durationMs < 100 {
		durationMs = 3000
	}
	transIn := formValueOr(r, "transition_in", "cut")
	transOut := formValueOr(r, "transition_out", "cut")
	if !validEditTransition(transIn) || !validEditTransition(transOut) {
		http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("Transitions must be cut, fade or dissolve.")+"&kind=error", http.StatusSeeOther)
		return
	}
	if _, err := s.store.AddEditItem(r.Context(), edit.ID, shotID, assetID, durationMs, transIn, transOut); err != nil {
		s.log.Error("add edit item", "err", err)
		http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("Could not add clip.")+"&kind=error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("Clip added.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleUpdateEditItem(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.loadVideoCtx(w, r); !ok {
		return
	}
	_ = r.ParseForm()
	itemID := chiURLParam(r, "itemID")
	durationMs := parseInt(r.FormValue("duration_s"), 3) * 1000
	if durationMs < 100 {
		durationMs = 3000
	}
	transIn := formValueOr(r, "transition_in", "cut")
	transOut := formValueOr(r, "transition_out", "cut")
	if !validEditTransition(transIn) || !validEditTransition(transOut) {
		http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("Transitions must be cut, fade or dissolve.")+"&kind=error", http.StatusSeeOther)
		return
	}
	err := s.store.UpdateEditItem(r.Context(), itemID, 0, durationMs, durationMs,
		transIn, transOut, parseInt(r.FormValue("revision"), 1))
	if err != nil {
		http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("Clip changed elsewhere; reloaded.")+"&kind=warning", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.editURL(r), http.StatusSeeOther)
}

func (s *Server) handleMoveEditItem(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.loadVideoCtx(w, r); !ok {
		return
	}
	_ = r.ParseForm()
	_ = s.store.MoveEditItem(r.Context(), chiURLParam(r, "itemID"), parseInt(r.FormValue("direction"), 1))
	http.Redirect(w, r, s.editURL(r), http.StatusSeeOther)
}

func (s *Server) handleDeleteEditItem(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.loadVideoCtx(w, r); !ok {
		return
	}
	_ = s.store.DeleteEditItem(r.Context(), chiURLParam(r, "itemID"))
	http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg("Clip removed from this cut (asset retained).")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleValidateEdit(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	edit, err := s.store.GetEdit(r.Context(), video.ID, chiURLParam(r, "editID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	items, _ := s.store.ListEditItems(r.Context(), edit.ID)
	var warnings []string
	if len(items) == 0 {
		warnings = append(warnings, "Edit has no clips.")
	}
	for _, it := range items {
		asset, err := s.store.GetAsset(r.Context(), it.AssetID)
		if err != nil {
			warnings = append(warnings, "Missing asset "+short(it.AssetID))
			continue
		}
		if asset.Checksum == "" {
			warnings = append(warnings, "Asset "+short(it.AssetID)+" has no checksum.")
		}
	}
	msg := "Edit validates: all clips present."
	kind := "success"
	if len(warnings) > 0 {
		msg = "Validation warnings: " + join(warnings)
		kind = "warning"
	}
	http.Redirect(w, r, s.editURL(r)+"?msg="+urlMsg(msg)+"&kind="+kind, http.StatusSeeOther)
}

func (s *Server) handleExportForm(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "edit"
	edit, err := s.store.GetEdit(r.Context(), video.ID, chiURLParam(r, "editID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	items, _ := s.store.ListEditItems(r.Context(), edit.ID)
	total := 0
	for _, it := range items {
		total += it.DurationMs
	}
	data := templates.EditData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Edit:    templates.EditView{ID: edit.ID, Name: edit.Name, Status: edit.Status, Revision: edit.Revision},
		TotalMs: total,
	}
	data.Shell.Flash = "Export profile"
	data.Shell.FlashKind = "info"
	s.renderPage(w, r, templates.ExportFormPage(data))
}

func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	edit, err := s.store.GetEdit(r.Context(), video.ID, chiURLParam(r, "editID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	user := UserFrom(r.Context())
	profile, _ := json.Marshal(map[string]any{
		"width": video.Width, "height": video.Height, "fps": video.FPSNum, "format": "mp4",
	})
	exp, err := s.store.CreateExport(r.Context(), edit.ID, "", string(profile))
	if err != nil {
		s.log.Error("create export", "err", err)
		s.notFound(w, r)
		return
	}
	job, err := s.store.EmitExportJob(r.Context(), project.WorkspaceID, exp.ID)
	if err != nil {
		s.log.Error("emit export job", "err", err)
		s.notFound(w, r)
		return
	}
	_ = s.store.SetExportJob(r.Context(), exp.ID, job.ID)
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "export.create", "export", exp.ID, "queued export", "{}")
	http.Redirect(w, r, s.exportURL(r, exp.ID), http.StatusSeeOther)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	exp, err := s.store.GetExport(r.Context(), chiURLParam(r, "exportID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	edit, err := s.store.GetEditByID(r.Context(), exp.EditID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	video, err := s.store.GetVideoByID(r.Context(), edit.VideoID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	project, err := s.store.GetProject(r.Context(), ws.ID, video.ProjectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	shell := s.shell(r, "Export")
	shell.Project = &templates.EntityRef{ID: project.ID, Title: project.Title}
	shell.Video = &templates.EntityRef{ID: video.ID, Title: video.Title}
	shell.ActiveNav = "edit"
	data := templates.ExportData{Shell: shell, Export: templates.ExportView{ID: exp.ID, Status: exp.Status, CreatedAt: exp.CreatedAt}}
	if exp.JobID.Valid {
		data.JobID = exp.JobID.String
	}
	if exp.ErrorMessage.Valid {
		data.Export.Error = exp.ErrorMessage.String
	}
	if exp.AssetID.Valid {
		if asset, err := s.store.GetAsset(r.Context(), exp.AssetID.String); err == nil {
			av := templates.AssetView{ID: asset.ID, Kind: asset.Kind, Role: asset.Role, MimeType: asset.MimeType, URL: "/assets/" + asset.ID, Checksum: asset.Checksum, SizeBytes: asset.SizeBytes, CreatedAt: asset.CreatedAt}
			data.Asset = &av
		}
	}
	s.renderPage(w, r, templates.ExportPage(data))
}

func (s *Server) editURL(r *http.Request) string { return s.videoBase(r) + "/edit" }
func (s *Server) exportURL(r *http.Request, id string) string {
	return s.videoBase(r) + "/exports/" + id
}

func short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}
