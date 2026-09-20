package web

import (
	"net/http"
	"strings"

	"vidbuild/internal/templates"
)

func (s *Server) handleWorkspaceList(w http.ResponseWriter, r *http.Request) {
	data := templates.WorkspaceListData{Shell: s.shell(r, "Workspaces")}
	s.renderPage(w, r, templates.WorkspaceListPage(data))
}

func (s *Server) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	kind := r.FormValue("kind")
	switch kind {
	case "personal", "client", "team":
	default:
		kind = "personal"
	}
	if name == "" {
		http.Redirect(w, r, "/workspaces?msg="+urlMsg("Workspace name is required.")+"&kind=error", http.StatusSeeOther)
		return
	}
	user := UserFrom(r.Context())
	ws, err := s.store.CreateWorkspace(r.Context(), user.ID, name, kind)
	if err != nil {
		s.log.Error("create workspace", "err", err)
		http.Redirect(w, r, "/workspaces?msg="+urlMsg("Could not create workspace.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "workspace.create", "workspace", ws.ID, "created workspace", "{}")
	http.Redirect(w, r, "/w/"+ws.ID, http.StatusSeeOther)
}

func (s *Server) handleWorkspaceDashboard(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	cards, err := s.projectCards(r, ws.ID)
	if err != nil {
		s.log.Error("list projects", "err", err)
	}
	s.renderPage(w, r, templates.WorkspaceDashboardPage(templates.DashboardData{
		Shell: s.shell(r, ws.Name), Projects: cards,
	}))
}

func (s *Server) handleProjectList(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	cards, err := s.projectCards(r, ws.ID)
	if err != nil {
		s.log.Error("list projects", "err", err)
	}
	shell := s.shell(r, "Projects")
	shell.ActiveNav = "projects"
	s.renderPage(w, r, templates.WorkspaceDashboardPage(templates.DashboardData{Shell: shell, Projects: cards}))
}

func (s *Server) projectCards(r *http.Request, workspaceID string) ([]templates.ProjectCard, error) {
	projects, err := s.store.ListProjects(r.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	cards := make([]templates.ProjectCard, 0, len(projects))
	for _, p := range projects {
		count, _ := s.store.CountVideosForProject(r.Context(), p.ID)
		cards = append(cards, templates.ProjectCard{
			ID: p.ID, Title: p.Title, Description: p.Description, Status: p.Status,
			VideoCount: count, UpdatedAt: p.UpdatedAt,
		})
	}
	return cards, nil
}

func (s *Server) handleWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "Workspace settings")
	shell.ActiveNav = "settings"
	s.renderPage(w, r, templates.SettingsPage(shell, templates.SettingsView{
		Name: ws.Name,
		Kind: ws.Kind,
		Role: RoleFrom(r.Context()),
	}))
}
