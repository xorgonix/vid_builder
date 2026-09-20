package web

import (
	"net/http"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/templates"
)

func (s *Server) handleModelProfiles(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "Model profiles")
	shell.ActiveNav = "settings"
	profiles, _ := s.store.ListModelProfiles(r.Context(), ws.ID)
	data := templates.ModelProfilesData{Shell: shell}
	for _, p := range profiles {
		data.Profiles = append(data.Profiles, templates.ProfileView{ID: p.ID, Name: p.Name, Task: p.Task, Checkpoint: p.ProfileCheckpoint()})
	}
	if _, err := comfyui.New(s.cfg.ComfyUI).Stats(r.Context()); err == nil {
		data.ComfyReady = true
		data.Checkpoints, _ = comfyui.New(s.cfg.ComfyUI).Checkpoints(r.Context())
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.ModelProfilesPage(data))
}

func (s *Server) handleCreateModelProfile(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	_ = r.ParseForm()
	name := formValueOr(r, "name", "New profile")
	task := formValueOr(r, "task", "image")
	checkpoint := r.FormValue("checkpoint")
	if _, err := s.store.CreateModelProfile(r.Context(), ws.ID, actorID(r), name, task, checkpoint); err != nil {
		s.log.Error("create model profile", "err", err)
		http.Redirect(w, r, "/w/"+ws.ID+"/model-profiles?msg="+urlMsg("Could not create profile.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "model_profile.create", "workspace", ws.ID, "created model profile", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/model-profiles?msg="+urlMsg("Model profile created.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleDeleteModelProfile(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	if err := s.store.DeleteModelProfile(r.Context(), ws.ID, chiURLParam(r, "profileID")); err != nil {
		s.log.Error("delete model profile", "err", err)
	}
	http.Redirect(w, r, "/w/"+ws.ID+"/model-profiles?msg="+urlMsg("Profile removed.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleSetProjectDefaultProfile(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	project, err := s.store.GetProject(r.Context(), ws.ID, chiURLParam(r, "p"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	profileID := r.FormValue("profile_id")
	if err := s.store.SetProjectDefaultProfile(r.Context(), project.ID, profileID); err != nil {
		s.log.Error("set project default profile", "err", err)
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "project.default_profile", "project", project.ID, "set default model profile", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/projects/"+project.ID+"?msg="+urlMsg("Default model profile updated.")+"&kind=success", http.StatusSeeOther)
}
