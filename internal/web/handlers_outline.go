package web

import (
	"net/http"
	"strings"

	"vidbuild/internal/templates"
)

func (s *Server) handleOutline(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "outline"
	sections, err := s.store.ListSections(r.Context(), video.ID)
	if err != nil {
		s.log.Error("list sections", "err", err)
	}
	var nodes []templates.SectionNode
	for _, sec := range sections {
		node := templates.SectionNode{ID: sec.ID, Title: sec.Title, Purpose: sec.Purpose, Position: sec.Position, Revision: sec.Revision, BudgetMs: sec.DurationBudgetMs}
		scenes, _ := s.store.ListScenes(r.Context(), sec.ID)
		for _, sc := range scenes {
			sceneNode := templates.SceneNode{ID: sc.ID, Title: sc.Title, Synopsis: sc.Synopsis, Purpose: sc.Purpose, Pace: sc.Pace, Reaction: sc.AudienceReaction, Position: sc.Position, Revision: sc.Revision, BudgetMs: sc.DurationBudgetMs}
			shots, _ := s.store.ListShots(r.Context(), sc.ID)
			for _, sh := range shots {
				if sh.Status == "archived" {
					continue
				}
				ready := 0
				for _, kind := range []string{"start_frame", "end_frame", "motion"} {
					if sp, err := s.store.GetCurrentSpec(r.Context(), sh.ID, kind); err == nil && sp != nil && (sp.PositivePrompt != "" || sp.StructuredSpecJSON != "{}") {
						ready++
					}
				}
				sceneNode.Shots = append(sceneNode.Shots, templates.ShotNode{
					ID: sh.ID, Title: sh.Title, Purpose: sh.Purpose, Treatment: sh.Treatment,
					Position: sh.Position, Revision: sh.Revision, DurationMs: sh.DurationTargetMs,
					Status: sh.Status, Readiness: sh.Readiness, SpecsReady: ready,
				})
			}
			node.Scenes = append(node.Scenes, sceneNode)
		}
		nodes = append(nodes, node)
	}
	data := templates.OutlineData{Shell: shell, Project: toProjectView(project), Video: toVideoView(video), Sections: nodes}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.OutlinePage(data))
}

func (s *Server) handleCreateSection(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "New section"
	}
	user := UserFrom(r.Context())
	if _, err := s.store.CreateSection(r.Context(), video.ID, nil, title, strings.TrimSpace(r.FormValue("purpose")), parseInt(r.FormValue("budget_s"), 0)*1000); err != nil {
		s.log.Error("create section", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "section.create", "video", video.ID, "created section", "{}")
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleUpdateSection(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	id := chiURLParam(r, "sectionID")
	err := s.store.UpdateSection(r.Context(), id, strings.TrimSpace(r.FormValue("title")), strings.TrimSpace(r.FormValue("purpose")), parseInt(r.FormValue("budget_s"), 0)*1000, parseInt(r.FormValue("revision"), 1))
	if err != nil {
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Section changed elsewhere; reloaded.")+"&kind=warning", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "section.update", "section", id, "updated section", "{}")
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleDeleteSection(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	id := chiURLParam(r, "sectionID")
	rehome := strings.TrimSpace(r.FormValue("rehome_to"))
	user := UserFrom(r.Context())
	if err := s.store.DeleteSection(r.Context(), id, rehome); err != nil {
		s.log.Error("delete section", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "section.delete", "section", id, "deleted section", "{}")
	_ = video
	http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Section removed; scenes rehomed.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleMoveSection(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.loadVideoCtx(w, r); !ok {
		return
	}
	_ = s.store.MoveSection(r.Context(), chiURLParam(r, "sectionID"), parseInt(r.FormValue("direction"), 1))
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleCreateScene(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "New scene"
	}
	user := UserFrom(r.Context())
	if _, err := s.store.CreateScene(r.Context(), chiURLParam(r, "sectionID"), title, strings.TrimSpace(r.FormValue("synopsis")), strings.TrimSpace(r.FormValue("purpose")), strings.TrimSpace(r.FormValue("pace")), strings.TrimSpace(r.FormValue("reaction")), parseInt(r.FormValue("budget_s"), 0)*1000); err != nil {
		s.log.Error("create scene", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "scene.create", "section", chiURLParam(r, "sectionID"), "created scene", "{}")
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleUpdateScene(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	id := chiURLParam(r, "sceneID")
	err := s.store.UpdateScene(r.Context(), id, strings.TrimSpace(r.FormValue("title")), strings.TrimSpace(r.FormValue("synopsis")), strings.TrimSpace(r.FormValue("purpose")), strings.TrimSpace(r.FormValue("pace")), strings.TrimSpace(r.FormValue("reaction")), parseInt(r.FormValue("budget_s"), 0)*1000, parseInt(r.FormValue("revision"), 1))
	if err != nil {
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Scene changed elsewhere; reloaded.")+"&kind=warning", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "scene.update", "scene", id, "updated scene", "{}")
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleDeleteScene(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	id := chiURLParam(r, "sceneID")
	user := UserFrom(r.Context())
	if err := s.store.DeleteScene(r.Context(), id); err != nil {
		s.log.Error("delete scene", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "scene.delete", "scene", id, "deleted scene", "{}")
	http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Scene removed.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleMoveScene(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.loadVideoCtx(w, r); !ok {
		return
	}
	_ = s.store.MoveScene(r.Context(), chiURLParam(r, "sceneID"), parseInt(r.FormValue("direction"), 1))
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleCreateShot(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "New shot"
	}
	user := UserFrom(r.Context())
	shot, err := s.store.CreateShot(r.Context(), chiURLParam(r, "sceneID"), user.ID, title, strings.TrimSpace(r.FormValue("purpose")), strings.TrimSpace(r.FormValue("treatment")), parseInt(r.FormValue("duration_s"), 0)*1000)
	if err != nil {
		s.log.Error("create shot", "err", err)
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Could not create shot.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "shot.create", "shot", shot.ID, "created shot and three specs", "{}")
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleUpdateShot(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	id := chiURLParam(r, "shotID")
	err := s.store.UpdateShot(r.Context(), id, strings.TrimSpace(r.FormValue("title")), strings.TrimSpace(r.FormValue("purpose")), strings.TrimSpace(r.FormValue("treatment")), parseInt(r.FormValue("duration_s"), 0)*1000, parseInt(r.FormValue("revision"), 1))
	if err != nil {
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Shot changed elsewhere; reloaded.")+"&kind=warning", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "shot.update", "shot", id, "updated shot", "{}")
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}

func (s *Server) handleDeleteShot(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	id := chiURLParam(r, "shotID")
	user := UserFrom(r.Context())
	if err := s.store.DeleteShot(r.Context(), id); err != nil {
		s.log.Error("delete shot", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "shot.archive", "shot", id, "archived shot", "{}")
	http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Shot archived.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleMoveShot(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.loadVideoCtx(w, r); !ok {
		return
	}
	_ = s.store.MoveShot(r.Context(), chiURLParam(r, "shotID"), parseInt(r.FormValue("direction"), 1))
	http.Redirect(w, r, s.outlineURL(r), http.StatusSeeOther)
}
