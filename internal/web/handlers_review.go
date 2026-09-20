package web

import (
	"database/sql"
	"net/http"
	"strings"

	"vidbuild/internal/templates"
)

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "review"
	data := templates.ReviewData{Shell: shell, Project: toProjectView(project), Video: toVideoView(video)}
	data.Groups = s.reviewGroups(r, video.ID)
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.ReviewPage(data))
}

func (s *Server) reviewGroups(r *http.Request, videoID string) []templates.ReviewGroup {
	var groups []templates.ReviewGroup
	sections, _ := s.store.ListSections(r.Context(), videoID)
	for _, sec := range sections {
		scenes, _ := s.store.ListScenes(r.Context(), sec.ID)
		for _, sc := range scenes {
			shots, _ := s.store.ListShots(r.Context(), sc.ID)
			for _, sh := range shots {
				if sh.Status == "archived" {
					continue
				}
				for _, role := range []string{"start_frame", "end_frame", "motion"} {
					assets, _ := s.store.ListAssetsForShotRole(r.Context(), sh.ID, role)
					sel, _ := s.store.GetCurrentSelection(r.Context(), sh.ID, role)
					if len(assets) == 0 && sel == nil {
						continue
					}
					g := templates.ReviewGroup{ShotID: sh.ID, ShotTitle: sh.Title, Role: role, RoleLabel: roleLabel(role)}
					g.Assets = s.assetViews(r.Context(), assets)
					if sel != nil {
						g.SelectedID = sel.AssetID
						g.Stale = sel.ShotRevision != sh.Revision
						if ap, err := s.store.LatestApproval(r.Context(), sel.AssetID); err == nil && ap != nil {
							g.Decision = ap.Decision
						}
					}
					groups = append(groups, g)
				}
			}
		}
	}
	return groups
}

func (s *Server) handleSelectAsset(w http.ResponseWriter, r *http.Request) {
	_, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	shotID := chiURLParam(r, "shotID")
	role := r.FormValue("role")
	assetID := r.FormValue("asset_id")
	if role != "start_frame" && role != "end_frame" && role != "motion" {
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Invalid role.")+"&kind=error", http.StatusSeeOther)
		return
	}
	shot, err := s.store.GetShot(r.Context(), shotID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shot.ID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	// The asset must actually belong to this shot and role.
	assets, _ := s.store.ListAssetsForShotRole(r.Context(), shotID, role)
	valid := false
	for _, a := range assets {
		if a.ID == assetID {
			valid = true
			break
		}
	}
	if !valid {
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("That asset is not a candidate for this shot.")+"&kind=error", http.StatusSeeOther)
		return
	}
	user := UserFrom(r.Context())
	if err := s.store.CreateSelection(r.Context(), shotID, role, assetID, user.ID, shot.Revision); err != nil {
		s.log.Error("select asset", "err", err)
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Could not select asset.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_, _ = s.store.RecomputeShotReadiness(r.Context(), shotID)
	_ = s.store.Audit(r.Context(), WorkspaceFrom(r.Context()).ID, user.ID, "asset.select", "shot", shotID, "selected asset for "+role, "{}")
	http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg(roleLabel(role)+" selection updated.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	assetID := chiURLParam(r, "assetID")
	asset, err := s.store.GetAsset(r.Context(), assetID)
	if err != nil || asset.WorkspaceID != ws.ID {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	decision := r.FormValue("decision")
	if decision != "approve" && decision != "reject" && decision != "approve_with_exception" {
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Invalid decision.")+"&kind=error", http.StatusSeeOther)
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if decision != "approve" && reason == "" {
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("A reason is required for rejection or exception approval.")+"&kind=error", http.StatusSeeOther)
		return
	}
	// Refuse to approve a deleted asset or a checksum that no longer matches.
	if asset.Checksum == "" {
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Asset has no verified checksum; cannot approve.")+"&kind=error", http.StatusSeeOther)
		return
	}
	user := UserFrom(r.Context())
	if err := s.store.CreateApproval(r.Context(), assetID, decision, reason, asset.Checksum, user.ID, 0); err != nil {
		s.log.Error("approve asset", "err", err)
		http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Could not record decision.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "asset.approval", "asset", assetID, decision, "{}")
	http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Decision recorded.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleAssess(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	assetID := chiURLParam(r, "assetID")
	asset, err := s.store.GetAsset(r.Context(), assetID)
	if err != nil || asset.WorkspaceID != ws.ID {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	status := r.FormValue("status")
	if status != "pass" && status != "review" && status != "fail" {
		status = "review"
	}
	user := UserFrom(r.Context())
	if err := s.store.CreateAssessment(r.Context(), assetID, "human", status, strings.TrimSpace(r.FormValue("summary")), sql.NullInt64{}); err != nil {
		s.log.Error("assess asset", "err", err)
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "asset.assess", "asset", assetID, "recorded assessment", "{}")
	http.Redirect(w, r, s.reviewURL(r)+"?msg="+urlMsg("Assessment recorded (advisory only).")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) reviewURL(r *http.Request) string { return s.videoBase(r) + "/review" }
