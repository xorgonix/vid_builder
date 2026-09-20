package web

import (
	"net/http"
	"strings"

	"vidbuild/internal/auth"
	"vidbuild/internal/templates"
)

func validRole(role string) bool {
	switch role {
	case "owner", "editor", "reviewer", "viewer":
		return true
	}
	return false
}

func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "Members")
	shell.ActiveNav = "settings"
	members, err := s.store.ListMembers(r.Context(), ws.ID)
	if err != nil {
		s.log.Error("list members", "err", err)
	}
	user := UserFrom(r.Context())
	data := templates.MembersData{Shell: shell, CanEdit: RoleFrom(r.Context()) == "owner"}
	for _, m := range members {
		data.Members = append(data.Members, templates.MemberView{
			UserID: m.UserID, Email: m.Email, DisplayName: m.DisplayName,
			Role: m.Role, Status: m.Status, JoinedAt: m.JoinedAt, IsSelf: m.UserID == user.ID,
		})
	}
	if msg := r.URL.Query().Get("msg"); msg != "" {
		data.Shell.Flash = msg
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.MembersPage(data))
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	role := r.FormValue("role")
	if !validRole(role) {
		role = "viewer"
	}
	target, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		http.Redirect(w, r, "/w/"+ws.ID+"/members?msg="+urlMsg("No user with that email. Create the account in Administration first.")+"&kind=error", http.StatusSeeOther)
		return
	}
	if err := s.store.AddMember(r.Context(), ws.ID, target.ID, role, actorID(r)); err != nil {
		s.log.Error("add member", "err", err)
		http.Redirect(w, r, "/w/"+ws.ID+"/members?msg="+urlMsg("That user is already a member.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "member.add", "user", target.ID, "added member as "+role, "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/members?msg="+urlMsg("Member added.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	targetID := chiURLParam(r, "userID")
	_ = r.ParseForm()
	role := r.FormValue("role")
	status := formValueOr(r, "status", "active")
	if !validRole(role) {
		role = "viewer"
	}
	// Never remove the last active owner.
	if role != "owner" || status != "active" {
		if m, err := s.store.RoleFor(r.Context(), ws.ID, targetID); err == nil && m == "owner" {
			if owners, _ := s.store.CountOwners(r.Context(), ws.ID); owners <= 1 {
				http.Redirect(w, r, "/w/"+ws.ID+"/members?msg="+urlMsg("Cannot demote or disable the last owner.")+"&kind=error", http.StatusSeeOther)
				return
			}
		}
	}
	if err := s.store.UpdateMemberRole(r.Context(), ws.ID, targetID, role, status); err != nil {
		s.log.Error("update member", "err", err)
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "member.update", "user", targetID, "role "+role+" status "+status, "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/members?msg="+urlMsg("Membership updated.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	targetID := chiURLParam(r, "userID")
	if m, err := s.store.RoleFor(r.Context(), ws.ID, targetID); err == nil && m == "owner" {
		if owners, _ := s.store.CountOwners(r.Context(), ws.ID); owners <= 1 {
			http.Redirect(w, r, "/w/"+ws.ID+"/members?msg="+urlMsg("Cannot remove the last owner.")+"&kind=error", http.StatusSeeOther)
			return
		}
	}
	if err := s.store.RemoveMember(r.Context(), ws.ID, targetID); err != nil {
		s.log.Error("remove member", "err", err)
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "member.remove", "user", targetID, "removed member", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/members?msg="+urlMsg("Member removed.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	shell := s.shell(r, "Users")
	users, err := s.store.ListAllUsers(r.Context())
	if err != nil {
		s.log.Error("list users", "err", err)
	}
	me := UserFrom(r.Context())
	data := templates.AdminUsersData{Shell: shell}
	for _, u := range users {
		data.Users = append(data.Users, templates.AdminUserView{
			ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Status: u.Status,
			IsAdmin: u.IsSystemAdmin, IsSelf: u.ID == me.ID, CreatedAt: u.CreatedAt,
		})
	}
	if msg := r.URL.Query().Get("msg"); msg != "" {
		data.Shell.Flash = msg
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.AdminUsersPage(data))
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	display := strings.TrimSpace(r.FormValue("display_name"))
	password := r.FormValue("password")
	isAdmin := r.FormValue("is_admin") != ""
	if email == "" || password == "" || display == "" {
		http.Redirect(w, r, "/admin/users?msg="+urlMsg("Email, display name and password are required.")+"&kind=error", http.StatusSeeOther)
		return
	}
	if _, err := s.store.GetUserByEmail(r.Context(), email); err == nil {
		http.Redirect(w, r, "/admin/users?msg="+urlMsg("A user with that email already exists.")+"&kind=error", http.StatusSeeOther)
		return
	}
	params := auth.DefaultParams(s.cfg.Auth.Argon2MemoryKB, s.cfg.Auth.Argon2Iterations, s.cfg.Auth.Argon2Parallelism)
	hash, err := auth.HashPassword(password, params)
	if err != nil {
		s.log.Error("hash password", "err", err)
		http.Redirect(w, r, "/admin/users?msg="+urlMsg("Could not create user.")+"&kind=error", http.StatusSeeOther)
		return
	}
	created, err := s.store.CreateUser(r.Context(), email, display, hash, isAdmin)
	if err != nil {
		s.log.Error("create user", "err", err)
		http.Redirect(w, r, "/admin/users?msg="+urlMsg("Could not create user.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), "", actorID(r), "user.create", "user", created.ID, "created user", "{}")
	http.Redirect(w, r, "/admin/users?msg="+urlMsg("User created.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	targetID := chiURLParam(r, "userID")
	_ = r.ParseForm()
	status := formValueOr(r, "status", "active")
	me := UserFrom(r.Context())
	if targetID == me.ID && status == "disabled" {
		http.Redirect(w, r, "/admin/users?msg="+urlMsg("You cannot disable your own account.")+"&kind=error", http.StatusSeeOther)
		return
	}
	if err := s.store.SetUserStatus(r.Context(), targetID, status); err != nil {
		s.log.Error("set user status", "err", err)
	}
	_ = s.store.Audit(r.Context(), "", actorID(r), "user.status", "user", targetID, "status "+status, "{}")
	http.Redirect(w, r, "/admin/users?msg="+urlMsg("User updated.")+"&kind=success", http.StatusSeeOther)
}
