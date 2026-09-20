package web

import (
	"net/http"
	"time"

	"vidbuild/internal/auth"
	"vidbuild/internal/db"
	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if UserFrom(r.Context()) != nil {
		http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
		return
	}
	data := templates.LoginData{CSRFToken: s.ensureLoginCSRF(w, r)}
	s.renderPage(w, r, templates.LoginPage(data))
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := r.FormValue("email")
	password := r.FormValue("password")
	token := s.ensureLoginCSRF(w, r)

	user, err := s.store.GetUserByEmail(r.Context(), email)
	valid := false
	if err == nil && user.Status == "active" {
		valid = auth.VerifyPassword(password, user.PasswordHash)
	} else {
		_ = auth.VerifyPassword(password, s.dummyHash)
	}
	if !valid {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderComponent(w, r, templates.LoginPage(templates.LoginData{
			CSRFToken: token, Error: "Invalid email or password.", Email: email,
		}))
		return
	}

	raw, hash := auth.NewToken()
	csrf := auth.NewCSRFSecret()
	expires := time.Now().Add(time.Duration(s.cfg.Auth.SessionTTLHours) * time.Hour)
	if _, err := s.store.CreateSession(r.Context(), user.ID, hash[:], csrf, db.Format(expires), r.UserAgent()); err != nil {
		s.log.Error("create session", "err", err)
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	_ = s.store.Audit(r.Context(), "", user.ID, "auth.login", "user", user.ID, "signed in", "{}")
	s.setSessionCookie(w, raw, expires)
	http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess := SessionFrom(r.Context()); sess != nil {
		_ = s.store.RevokeSession(r.Context(), sess.ID)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	user := UserFrom(r.Context())
	sessions, err := s.store.ListSessionsForUser(r.Context(), user.ID)
	if err != nil {
		s.log.Error("list sessions", "err", err)
	}
	s.renderPage(w, r, templates.AccountPage(s.shell(r, "Account"), templates.AccountView{
		Email:    user.Email,
		Name:     user.DisplayName,
		IsAdmin:  user.IsSystemAdmin,
		Status:   user.Status,
		JoinedAt: user.CreatedAt,
	}, toSessionViews(sessions)))
}

func (s *Server) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	user := UserFrom(r.Context())
	id := chiURLParam(r, "id")
	// Only the owner (or a system admin) may revoke; verify ownership.
	sessions, err := s.store.ListSessionsForUser(r.Context(), user.ID)
	if err != nil {
		s.log.Error("list sessions", "err", err)
	}
	owned := false
	for _, sess := range sessions {
		if sess.ID == id {
			owned = true
			break
		}
	}
	if !owned && !user.IsSystemAdmin {
		s.forbidden(w, r)
		return
	}
	if err := s.store.RevokeSession(r.Context(), id); err != nil {
		s.log.Error("revoke session", "err", err)
	}
	http.Redirect(w, r, "/account?msg="+urlMsg("Session revoked.")+"&kind=success", http.StatusSeeOther)
}

func toSessionViews(in []store.SessionInfo) []templates.SessionView {
	out := make([]templates.SessionView, 0, len(in))
	for _, s := range in {
		sv := templates.SessionView{
			ID:        s.ID,
			CreatedAt: s.CreatedAt,
			LastSeen:  s.LastSeen,
			ExpiresAt: s.ExpiresAt,
		}
		if s.RevokedAt.Valid {
			sv.Revoked = true
		}
		if s.UserAgent.Valid {
			sv.UserAgent = truncate(s.UserAgent.String, 80)
		}
		out = append(out, sv)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
