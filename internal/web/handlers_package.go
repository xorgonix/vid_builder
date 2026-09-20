package web

import (
	"net/http"

	"vidbuild/internal/templates"
)

func (s *Server) handleProjectPackage(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	project, err := s.store.GetProject(r.Context(), ws.ID, chiURLParam(r, "p"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	shell := s.shell(r, project.Title+" · Package")
	shell.Project = &templates.EntityRef{ID: project.ID, Title: project.Title}
	data := templates.PackageData{Shell: shell, Project: toProjectView(project)}

	count := func(label, q string) {
		var n int
		if err := s.store.QueryRowContext(r.Context(), q, project.ID).Scan(&n); err == nil {
			data.Counts = append(data.Counts, templates.PackageCount{Label: label, Value: n})
		}
	}
	count("Videos", `SELECT COUNT(*) FROM videos WHERE project_id=?`)
	count("Sections", `SELECT COUNT(*) FROM sections WHERE video_id IN (SELECT id FROM videos WHERE project_id=?)`)
	count("Scenes", `SELECT COUNT(*) FROM scenes WHERE section_id IN (SELECT id FROM sections WHERE video_id IN (SELECT id FROM videos WHERE project_id=?))`)
	count("Shots", `SELECT COUNT(*) FROM shots WHERE scene_id IN (SELECT id FROM scenes WHERE section_id IN (SELECT id FROM sections WHERE video_id IN (SELECT id FROM videos WHERE project_id=?)))`)
	count("Characters", `SELECT COUNT(*) FROM characters WHERE project_id=?`)
	count("Locations", `SELECT COUNT(*) FROM locations WHERE project_id=?`)
	count("Style rules", `SELECT COUNT(*) FROM style_rules WHERE project_id=?`)
	count("Briefs", `SELECT COUNT(*) FROM project_briefs WHERE project_id=?`)
	count("Proposals", `SELECT COUNT(*) FROM proposals WHERE project_id=?`)
	_ = s.store.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM assets WHERE project_id=? AND deleted_at IS NULL`, project.ID).Scan(&data.MediaCount)

	if asset, err := s.store.LatestPackageAsset(r.Context(), project.ID); err == nil && asset != nil {
		av := templates.AssetView{ID: asset.ID, Kind: asset.Kind, Role: asset.Role, MimeType: asset.MimeType, URL: "/assets/" + asset.ID, Checksum: asset.Checksum, SizeBytes: asset.SizeBytes, CreatedAt: asset.CreatedAt}
		data.Latest = &av
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.PackagePage(data))
}

func (s *Server) handleQueuePackage(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	project, err := s.store.GetProject(r.Context(), ws.ID, chiURLParam(r, "p"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	user := UserFrom(r.Context())
	job, err := s.store.EmitPackageJob(r.Context(), ws.ID, project.ID)
	if err != nil {
		s.log.Error("emit package job", "err", err)
		s.notFound(w, r)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "project.package", "project", project.ID, "queued package", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"/package?msg="+urlMsg("Package queued ("+job.ID[:8]+"). Refresh after the worker finishes.")+"&kind=success", http.StatusSeeOther)
}
