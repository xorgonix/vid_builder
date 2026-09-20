package web

import (
	"net/http"
	"strings"

	"vidbuild/internal/templates"
)

func (s *Server) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	data := templates.ProjectNewData{
		Shell:  s.shell(r, "New project"),
		Values: map[string]string{"format": "landscape"},
	}
	s.renderPage(w, r, templates.ProjectNewPage(data))
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	values := map[string]string{
		"title":       strings.TrimSpace(r.FormValue("title")),
		"description": strings.TrimSpace(r.FormValue("description")),
		"video_title": strings.TrimSpace(r.FormValue("video_title")),
		"purpose":     strings.TrimSpace(r.FormValue("purpose")),
		"audience":    strings.TrimSpace(r.FormValue("audience")),
		"format":      normalizeFormat(r.FormValue("format")),
		"duration_s":  strings.TrimSpace(r.FormValue("duration_s")),
	}
	if values["title"] == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderComponent(w, r, templates.ProjectNewPage(templates.ProjectNewData{
			Shell: s.shell(r, "New project"), Error: "Project title is required.", Values: values,
		}))
		return
	}
	ws := WorkspaceFrom(r.Context())
	user := UserFrom(r.Context())
	project, video, err := s.store.CreateProjectWithVideo(r.Context(), ws.ID, user.ID,
		values["title"], values["description"], values["video_title"], values["purpose"], values["format"])
	if err != nil {
		s.log.Error("create project", "err", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderComponent(w, r, templates.ProjectNewPage(templates.ProjectNewData{
			Shell: s.shell(r, "New project"), Error: "Could not create project.", Values: values,
		}))
		return
	}
	// Optional audience and target duration on the freshly created video.
	if values["audience"] != "" || values["duration_s"] != "" {
		_ = s.store.SetVideoAudienceAndDuration(r.Context(), project.ID, video.ID, values["audience"], parseInt(values["duration_s"], 0)*1000)
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "project.create", "project", project.ID, "created project and first video", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"/videos/"+video.ID, http.StatusSeeOther)
}

func (s *Server) handleProjectDashboard(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	videos, err := s.store.ListVideos(r.Context(), project.ID)
	if err != nil {
		s.log.Error("list videos", "err", err)
	}
	shell := s.shell(r, project.Title)
	shell.Project = &templates.EntityRef{ID: project.ID, Title: project.Title}
	views := make([]templates.VideoView, 0, len(videos))
	for _, v := range videos {
		views = append(views, templates.VideoView{ID: v.ID, Title: v.Title, Purpose: v.Purpose, Format: v.Format, Status: v.Status, Width: v.Width, Height: v.Height})
	}
	data := templates.ProjectData{
		Shell:   shell,
		Project: templates.ProjectView{ID: project.ID, Title: project.Title, Description: project.Description, Status: project.Status},
		Videos:  views,
	}
	if profiles, err := s.store.ListModelProfiles(r.Context(), ws.ID); err == nil {
		for _, p := range profiles {
			data.Profiles = append(data.Profiles, templates.ProfileOption{ID: p.ID, Name: p.Name, Checkpoint: p.ProfileCheckpoint()})
		}
	}
	data.DefaultProfileID = s.store.GetProjectDefaultProfile(r.Context(), project.ID)
	s.renderPage(w, r, templates.ProjectPage(data))
}

func (s *Server) handleVideoCreate(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "Untitled deliverable"
	}
	format := normalizeFormat(r.FormValue("format"))
	user := UserFrom(r.Context())
	video, err := s.store.CreateVideo(r.Context(), project.ID, user.ID, title, "", format)
	if err != nil {
		s.log.Error("create video", "err", err)
		http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"?msg="+urlMsg("Could not create video.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "video.create", "video", video.ID, "created video", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"/videos/"+video.ID, http.StatusSeeOther)
}

func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	projectID := chiURLParam(r, "p")
	videoID := chiURLParam(r, "v")
	project, err := s.store.GetProject(r.Context(), ws.ID, projectID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	video, err := s.store.GetVideo(r.Context(), project.ID, videoID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	sections, scenes, shots, err := s.store.VideoStructureCounts(r.Context(), video.ID)
	if err != nil {
		s.log.Error("structure counts", "err", err)
	}
	hasBrief, _ := s.store.HasCurrentBrief(r.Context(), project.ID)
	shell := s.shell(r, video.Title)
	shell.Project = &templates.EntityRef{ID: project.ID, Title: project.Title}
	shell.Video = &templates.EntityRef{ID: video.ID, Title: video.Title}
	s.renderPage(w, r, templates.VideoPage(templates.VideoData{
		Shell:    shell,
		Project:  templates.ProjectView{ID: project.ID, Title: project.Title},
		Video:    templates.VideoView{ID: video.ID, Title: video.Title, Purpose: video.Purpose, Format: video.Format, Status: video.Status, Width: video.Width, Height: video.Height},
		Sections: sections, Scenes: scenes, Shots: shots, BriefCurrent: hasBrief,
	}))
}

func normalizeFormat(f string) string {
	switch f {
	case "vertical", "square", "landscape":
		return f
	default:
		return "landscape"
	}
}
