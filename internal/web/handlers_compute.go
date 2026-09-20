package web

import (
	"net/http"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/templates"
)

const defaultPerCandidateMs = 8000

func (s *Server) targetEstimates(r *http.Request, draftCount, draftCandidates int) []templates.TargetEstimate {
	ws := WorkspaceFrom(r.Context())
	targets, _ := s.store.ListExecutionTargets(r.Context(), ws.ID)
	if len(targets) == 0 {
		if _, err := s.store.EnsureLocalComfyTarget(r.Context(), ws.ID, actorID(r)); err == nil {
			targets, _ = s.store.ListExecutionTargets(r.Context(), ws.ID)
		}
	}
	var out []templates.TargetEstimate
	for _, t := range targets {
		avg, samples := s.store.AvgObservationMs(r.Context(), t.ID)
		note := ""
		if samples == 0 {
			avg = defaultPerCandidateMs
			note = "assumed (no observations yet)"
		} else {
			note = "measured"
		}
		out = append(out, templates.TargetEstimate{
			ID: t.ID, Name: t.Name, Kind: t.Kind, Enabled: t.Enabled,
			AvgMs: avg, Samples: samples, EstimatedMs: avg * int64(draftCandidates), Currency: "USD", Note: note,
		})
	}
	return out
}

func (s *Server) handleCompute(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "produce"
	data := templates.ComputeData{Shell: shell, Project: toProjectView(project), Video: toVideoView(video)}
	drafts, _ := s.store.ListDraftRequests(r.Context(), video.ID)
	totalCandidates := 0
	for _, req := range drafts {
		shot, _ := s.store.GetShot(r.Context(), req.ShotID)
		rv := templates.RequestView{ID: req.ID, Role: req.OutputRole, Tier: req.QualityTier, Candidates: req.CandidateCount, Status: req.Status, CreatedAt: req.CreatedAt}
		if shot != nil {
			rv.ShotTitle = shot.Title
		}
		data.Drafts = append(data.Drafts, rv)
		totalCandidates += req.CandidateCount
	}
	data.Targets = s.targetEstimates(r, len(drafts), totalCandidates)
	campaigns, _ := s.store.ListCampaigns(r.Context(), project.WorkspaceID, video.ID)
	for _, c := range campaigns {
		cv := templates.CampaignView{ID: c.ID, Name: c.Name, Status: c.Status, EstimatedMs: c.EstimatedMs, ActualMs: c.ActualMs, CreatedAt: c.CreatedAt}
		if c.ExecutionTargetID.Valid {
			if t, err := s.store.GetExecutionTarget(r.Context(), c.ExecutionTargetID.String); err == nil {
				cv.TargetName = t.Name
			}
		}
		if items, err := s.store.ListCampaignItems(r.Context(), c.ID); err == nil {
			cv.Items = len(items)
		}
		data.Campaigns = append(data.Campaigns, cv)
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.ComputePage(data))
}

func (s *Server) handleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	name := formValueOr(r, "name", "Render campaign")
	targetID := r.FormValue("target_id")
	ws := WorkspaceFrom(r.Context())
	user := UserFrom(r.Context())
	camp, err := s.store.CreateCampaign(r.Context(), ws.ID, project.ID, video.ID, name, targetID, user.ID)
	if err != nil {
		s.log.Error("create campaign", "err", err)
		s.notFound(w, r)
		return
	}
	drafts, _ := s.store.ListDraftRequests(r.Context(), video.ID)
	for _, req := range drafts {
		_ = s.store.AddCampaignItem(r.Context(), camp.ID, req.ID)
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "campaign.create", "campaign", camp.ID, "created campaign", "{}")
	http.Redirect(w, r, s.campaignURL(r, camp.ID), http.StatusSeeOther)
}

func (s *Server) handleCampaign(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "produce"
	ws := WorkspaceFrom(r.Context())
	camp, err := s.store.GetCampaign(r.Context(), ws.ID, chiURLParam(r, "campaignID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data := templates.CampaignPageData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Campaign:  templates.CampaignView{ID: camp.ID, Name: camp.Name, Status: camp.Status, EstimatedMs: camp.EstimatedMs, ActualMs: camp.ActualMs, CreatedAt: camp.CreatedAt},
		CanLaunch: camp.Status == "ready" || camp.Status == "draft" || camp.Status == "paused",
	}
	if camp.ExecutionTargetID.Valid {
		if t, err := s.store.GetExecutionTarget(r.Context(), camp.ExecutionTargetID.String); err == nil {
			data.Campaign.TargetName = t.Name
			avg, samples := s.store.AvgObservationMs(r.Context(), t.ID)
			if samples == 0 {
				avg = defaultPerCandidateMs
			}
			data.Target = templates.TargetEstimate{ID: t.ID, Name: t.Name, Kind: t.Kind, Enabled: t.Enabled, AvgMs: avg, Samples: samples}
		}
	}
	items, _ := s.store.ListCampaignItems(r.Context(), camp.ID)
	for _, it := range items {
		req, err := s.store.GetRequest(r.Context(), it.GenerationRequestID)
		if err != nil {
			continue
		}
		shot, _ := s.store.GetShot(r.Context(), req.ShotID)
		iv := templates.CampaignItemView{RequestID: req.ID, Role: req.OutputRole, Candidates: req.CandidateCount, Status: it.Status}
		if shot != nil {
			iv.ShotTitle = shot.Title
		}
		iv.EstimatedMs = data.Target.AvgMs * int64(req.CandidateCount)
		data.Items = append(data.Items, iv)
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.CampaignPage(data))
}

func (s *Server) handleCampaignPreflight(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	camp, err := s.store.GetCampaign(r.Context(), ws.ID, chiURLParam(r, "campaignID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	items, _ := s.store.ListCampaignItems(r.Context(), camp.ID)
	if len(items) == 0 {
		http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Campaign has no items.")+"&kind=error", http.StatusSeeOther)
		return
	}
	// Verify the target and, for local ComfyUI, its availability.
	targetName := ""
	if camp.ExecutionTargetID.Valid {
		if t, err := s.store.GetExecutionTarget(r.Context(), camp.ExecutionTargetID.String); err == nil {
			targetName = t.Name
			if t.Kind == "local_comfy" {
				client := comfyui.New(s.cfg.ComfyUI)
				if _, err := client.Stats(r.Context()); err != nil {
					http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Preflight failed: ComfyUI unreachable: "+err.Error())+"&kind=error", http.StatusSeeOther)
					return
				}
			}
		}
	}
	// Every item must still have its workflow version.
	for _, it := range items {
		req, err := s.store.GetRequest(r.Context(), it.GenerationRequestID)
		if err != nil || !req.WorkflowVersionID.Valid {
			http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Preflight failed: an item has no workflow.")+"&kind=error", http.StatusSeeOther)
			return
		}
	}
	_ = s.store.SetCampaignStatus(r.Context(), camp.ID, "ready", 0, 0)
	_ = targetName
	http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Preflight passed.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleCampaignLaunch(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	camp, err := s.store.GetCampaign(r.Context(), ws.ID, chiURLParam(r, "campaignID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if camp.Status != "ready" && camp.Status != "draft" && camp.Status != "paused" {
		http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Campaign cannot launch from status "+camp.Status+".")+"&kind=error", http.StatusSeeOther)
		return
	}
	items, _ := s.store.ListCampaignItems(r.Context(), camp.ID)
	user := UserFrom(r.Context())
	launched := 0
	var totalMs int64
	for _, it := range items {
		if it.Status == "completed" || it.Status == "running" {
			continue
		}
		if _, err := s.store.EnqueueGenerationJob(r.Context(), ws.ID, it.GenerationRequestID, camp.ID); err != nil {
			s.log.Error("enqueue campaign item", "err", err)
			continue
		}
		launched++
	}
	if camp.ExecutionTargetID.Valid {
		tgt := camp.ExecutionTargetID.String
		avg, samples := s.store.AvgObservationMs(r.Context(), tgt)
		if samples == 0 {
			avg = defaultPerCandidateMs
		}
		reqs, _ := s.store.ListDraftRequests(r.Context(), camp.VideoID.String)
		for _, req := range reqs {
			totalMs += avg * int64(req.CandidateCount)
		}
	}
	_ = s.store.SetCampaignStatus(r.Context(), camp.ID, "running", totalMs, 0)
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "campaign.launch", "campaign", camp.ID, "launched campaign", "{}")
	http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Campaign launched; jobs queued.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleCampaignPause(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	camp, err := s.store.GetCampaign(r.Context(), ws.ID, chiURLParam(r, "campaignID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.store.SetCampaignStatus(r.Context(), camp.ID, "paused", camp.EstimatedMs, camp.EstimatedMicros)
	http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Campaign paused; running work continues.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleCampaignCancel(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	camp, err := s.store.GetCampaign(r.Context(), ws.ID, chiURLParam(r, "campaignID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.store.SetCampaignStatus(r.Context(), camp.ID, "cancelled", camp.EstimatedMs, camp.EstimatedMicros)
	http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Campaign cancelled.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleCampaignReconcile(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	camp, err := s.store.GetCampaign(r.Context(), ws.ID, chiURLParam(r, "campaignID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	// Actual time = sum of successful observations for the campaign's requests.
	actualMs := parseInt64(r.FormValue("actual_ms"), 0)
	if actualMs == 0 && camp.ExecutionTargetID.Valid {
		avg, samples := s.store.AvgObservationMs(r.Context(), camp.ExecutionTargetID.String)
		if samples > 0 {
			items, _ := s.store.ListCampaignItems(r.Context(), camp.ID)
			actualMs = avg * int64(len(items))
		}
	}
	_ = s.store.ReconcileCampaign(r.Context(), camp.ID, actualMs, 0)
	http.Redirect(w, r, s.campaignURL(r, camp.ID)+"?msg="+urlMsg("Campaign reconciled.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleExecutionTargets(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "Execution targets")
	shell.ActiveNav = "settings"
	targets, _ := s.store.ListExecutionTargets(r.Context(), ws.ID)
	var views []templates.TargetEstimate
	for _, t := range targets {
		avg, samples := s.store.AvgObservationMs(r.Context(), t.ID)
		views = append(views, templates.TargetEstimate{ID: t.ID, Name: t.Name, Kind: t.Kind, Enabled: t.Enabled, AvgMs: avg, Samples: samples})
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		shell.Flash = m
		shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.TargetsPage(shell, views))
}

func (s *Server) handleCreateExecutionTarget(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	_ = r.ParseForm()
	name := formValueOr(r, "name", "New target")
	kind := formValueOr(r, "kind", "remote_agent")
	user := UserFrom(r.Context())
	if _, err := s.store.CreateExecutionTarget(r.Context(), ws.ID, user.ID, name, kind); err != nil {
		s.log.Error("create target", "err", err)
	}
	http.Redirect(w, r, "/w/"+ws.ID+"/execution-targets?msg="+urlMsg("Target recorded (metadata only).")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) campaignURL(r *http.Request, id string) string {
	return s.videoBase(r) + "/campaigns/" + id
}
