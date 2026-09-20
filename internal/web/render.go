package web

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"vidbuild/internal/auth"
	"vidbuild/internal/templates"
)

func (s *Server) renderComponent(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		s.log.Error("render failed", "err", err, "path", r.URL.Path)
	}
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, c templ.Component) {
	s.renderComponent(w, r, c)
}

func errorPage(title, detail string, code int) templ.Component {
	return templates.ErrorPage(code, title, detail)
}

func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}

// shell builds the common chrome data for a page.
func (s *Server) shell(r *http.Request, title string) templates.ShellData {
	data := templates.ShellData{
		Title:     title,
		CSRFToken: csrfToken(r),
	}
	if u := UserFrom(r.Context()); u != nil {
		data.UserName = u.DisplayName
		data.IsAdmin = u.IsSystemAdmin
	}
	if ws := WorkspaceFrom(r.Context()); ws != nil {
		data.CurrentWorkspace = &templates.WorkspaceRef{ID: ws.ID, Name: ws.Name, Slug: ws.Slug, Kind: ws.Kind, Role: RoleFrom(r.Context())}
	}
	user := UserFrom(r.Context())
	if user != nil {
		workspaces, roles, err := s.store.ListWorkspacesForUser(r.Context(), user.ID)
		if err == nil {
			for _, w := range workspaces {
				data.Workspaces = append(data.Workspaces, templates.WorkspaceRef{ID: w.ID, Name: w.Name, Slug: w.Slug, Kind: w.Kind, Role: roles[w.ID]})
			}
		}
	}
	return data
}

func csrfToken(r *http.Request) string {
	if sess := SessionFrom(r.Context()); sess != nil {
		return auth.CSRFToken(sess.CSRFSecret)
	}
	return ""
}

// flash reads a one-off flash message from the query string.
func flashFrom(r *http.Request) (msg, kind string) {
	return r.URL.Query().Get("msg"), r.URL.Query().Get("kind")
}
