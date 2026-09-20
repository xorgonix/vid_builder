package web

import (
	"net/http"

	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

// loadVideoCtx resolves the workspace-scoped project and video for a handler.
func (s *Server) loadVideoCtx(w http.ResponseWriter, r *http.Request) (*store.Project, *store.Video, templates.ShellData, bool) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	videoID := chiURLParam(r, "v")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return nil, nil, templates.ShellData{}, false
	}
	video, err := s.store.GetVideo(r.Context(), project.ID, videoID)
	if err != nil {
		s.notFound(w, r)
		return nil, nil, templates.ShellData{}, false
	}
	shell := s.shell(r, video.Title)
	shell.Project = &templates.EntityRef{ID: project.ID, Title: project.Title}
	shell.Video = &templates.EntityRef{ID: video.ID, Title: video.Title}
	return project, video, shell, true
}

func toProjectView(p *store.Project) templates.ProjectView {
	return templates.ProjectView{ID: p.ID, Title: p.Title, Description: p.Description, Status: p.Status}
}

func toVideoView(v *store.Video) templates.VideoView {
	return templates.VideoView{ID: v.ID, Title: v.Title, Purpose: v.Purpose, Format: v.Format, Status: v.Status, Width: v.Width, Height: v.Height}
}
