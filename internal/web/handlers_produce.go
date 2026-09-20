package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

func roleLabel(role string) string {
	switch role {
	case "start_frame":
		return "Start frame"
	case "end_frame":
		return "End frame"
	case "motion":
		return "Motion"
	default:
		return role
	}
}

// bundledWorkflow returns the latest bundled workflow package/version.
func (s *Server) bundledWorkflow(r *http.Request) (pkg store.WorkflowPackage, version *store.WorkflowVersion, bindings []store.WorkflowBinding, err error) {
	pkgs, err := s.store.ListWorkflowPackages(r.Context())
	if err != nil {
		return pkg, nil, nil, err
	}
	for _, p := range pkgs {
		if p.LatestVersion.Valid {
			pkg = p
			break
		}
	}
	if !pkg.LatestVersion.Valid {
		return pkg, nil, nil, fmt.Errorf("no workflow available")
	}
	version, err = s.store.GetWorkflowVersion(r.Context(), pkg.LatestVersion.String)
	if err != nil {
		return pkg, nil, nil, err
	}
	bindings, _ = s.store.ListBindings(r.Context(), version.ID)
	return pkg, version, bindings, nil
}

func (s *Server) handleProduce(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "produce"
	data := templates.ProduceData{Shell: shell, Project: toProjectView(project), Video: toVideoView(video)}

	sections, _ := s.store.ListSections(r.Context(), video.ID)
	for _, sec := range sections {
		scenes, _ := s.store.ListScenes(r.Context(), sec.ID)
		for _, sc := range scenes {
			shots, _ := s.store.ListShots(r.Context(), sc.ID)
			for _, sh := range shots {
				if sh.Status == "archived" {
					continue
				}
				rs := templates.ReadyShot{ShotID: sh.ID, Title: sh.Title, Purpose: sh.Purpose, SceneTitle: sc.Title, Readiness: sh.Readiness}
				if sp, err := s.store.GetCurrentSpec(r.Context(), sh.ID, "start_frame"); err == nil && sp != nil && specHasContent(sp) {
					rs.StartReady = true
				}
				if sp, err := s.store.GetCurrentSpec(r.Context(), sh.ID, "end_frame"); err == nil && sp != nil && specHasContent(sp) {
					rs.EndReady = true
				}
				if sp, err := s.store.GetCurrentSpec(r.Context(), sh.ID, "motion"); err == nil && sp != nil && specHasContent(sp) {
					rs.MotionReady = true
				}
				data.Ready = append(data.Ready, rs)
			}
		}
	}
	requests, _ := s.store.ListRequestsForVideo(r.Context(), video.ID)
	for _, req := range requests {
		shot, _ := s.store.GetShot(r.Context(), req.ShotID)
		rv := templates.RequestView{ID: req.ID, ShotID: req.ShotID, Role: req.OutputRole, Tier: req.QualityTier, Candidates: req.CandidateCount, Status: req.Status, CreatedAt: req.CreatedAt}
		if shot != nil {
			rv.ShotTitle = shot.Title
		}
		if job, err := s.store.GetJobForRequest(r.Context(), req.ID); err == nil && job != nil {
			rv.JobID = job.ID
		}
		data.Requests = append(data.Requests, rv)
	}
	jobs, _ := s.store.ListJobsForVideo(r.Context(), video.ID)
	for _, j := range jobs {
		data.Jobs = append(data.Jobs, templates.JobView{ID: j.ID, Type: j.JobType, State: j.State, AggregateID: j.AggregateID, CreatedAt: j.CreatedAt})
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.ProducePage(data))
}

func specHasContent(sp *store.ShotSpec) bool {
	return sp.PositivePrompt != "" || sp.StructuredSpecJSON != "{}"
}

func (s *Server) handleGenerateForm(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "produce"
	role := chiURLParam(r, "role")
	if role != "start_frame" && role != "end_frame" && role != "motion" {
		s.notFound(w, r)
		return
	}
	shotID := chiURLParam(r, "shotID")
	shot, err := s.store.GetShot(r.Context(), shotID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shot.ID, video.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	data := templates.GenerateData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Shot: templates.ShotNode{ID: shot.ID, Title: shot.Title, Purpose: shot.Purpose, Revision: shot.Revision},
		Role: role, RoleLabel: roleLabel(role), Width: video.Width, Height: video.Height, SeedPolicy: "random",
	}
	if spec, err := s.store.GetCurrentSpec(r.Context(), shot.ID, role); err == nil && spec != nil {
		data.Positive = spec.PositivePrompt
		data.Negative = spec.NegativePrompt
	}
	if pkg, version, _, err := s.bundledWorkflow(r); err == nil {
		data.WorkflowID = version.ID
		data.WorkflowName = pkg.Name
	}
	// Selectable validated workflow versions (bundled and imported).
	if versions, err := s.store.ListSelectableVersions(r.Context()); err == nil {
		for _, v := range versions {
			name := "Workflow"
			if p, err := s.store.GetWorkflowPackage(r.Context(), v.WorkflowPackageID); err == nil {
				name = p.Name
			}
			data.Workflows = append(data.Workflows, templates.WorkflowOption{ID: v.ID, Name: name, Version: v.SemanticVersion, Status: v.Status})
			if data.WorkflowID == "" {
				data.WorkflowID = v.ID
				data.WorkflowName = name
			}
		}
	}
	// Capability preflight.
	client := comfyui.New(s.cfg.ComfyUI)
	if stats, err := client.Stats(r.Context()); err != nil {
		data.ComfyError = err.Error()
	} else {
		data.ComfyReady = true
		data.DefaultModel = firstCheckpoint(r, client)
		_ = stats
		if cps, err := client.Checkpoints(r.Context()); err == nil {
			data.Checkpoints = cps
		}
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.GeneratePage(data))
}

func firstCheckpoint(r *http.Request, client *comfyui.Client) string {
	cps, err := client.Checkpoints(r.Context())
	if err != nil || len(cps) == 0 {
		return ""
	}
	for _, cp := range cps {
		if cp == "photon_v1.safetensors" {
			return cp
		}
	}
	return cps[0]
}

func (s *Server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	shotID := r.FormValue("shot_id")
	role := r.FormValue("role")
	if role != "start_frame" && role != "end_frame" && role != "motion" {
		http.Redirect(w, r, s.videoBase(r)+"/produce?msg="+urlMsg("Invalid output role.")+"&kind=error", http.StatusSeeOther)
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
	spec, err := s.store.GetCurrentSpec(r.Context(), shot.ID, role)
	if err != nil {
		http.Redirect(w, r, s.videoBase(r)+"/produce?msg="+urlMsg("Shot has no specification for that role.")+"&kind=error", http.StatusSeeOther)
		return
	}
	var versionID string
	if chosen := r.FormValue("workflow_version_id"); chosen != "" {
		if v, err := s.store.GetWorkflowVersion(r.Context(), chosen); err == nil && v.Status == "validated" {
			versionID = v.ID
		}
	}
	if versionID == "" {
		_, version, _, err := s.bundledWorkflow(r)
		if err != nil {
			http.Redirect(w, r, s.videoBase(r)+"/produce?msg="+urlMsg("No validated workflow available.")+"&kind=error", http.StatusSeeOther)
			return
		}
		versionID = version.ID
	}
	candidates := parseInt(r.FormValue("candidates"), 1)
	if candidates < 1 {
		candidates = 1
	}
	payload := map[string]any{
		"positive":    r.FormValue("positive"),
		"negative":    r.FormValue("negative"),
		"width":       parseInt(r.FormValue("width"), video.Width),
		"height":      parseInt(r.FormValue("height"), video.Height),
		"seed":        int64(parseInt(r.FormValue("seed"), 0)),
		"seed_policy": formValueOr(r, "seed_policy", "random"),
		"checkpoint":  r.FormValue("checkpoint"),
	}
	raw, _ := json.Marshal(payload)
	user := UserFrom(r.Context())
	ws := WorkspaceFrom(r.Context())
	if _, err := s.store.EnsureLocalComfyTarget(r.Context(), ws.ID, user.ID); err != nil {
		s.log.Error("ensure target", "err", err)
	}
	req, _, job, err := s.store.CreateGenerationRequest(r.Context(), store.RequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: video.ID, ShotID: shot.ID, ShotSpecID: spec.ID,
		SpecVersion: spec.Version, OutputRole: role, QualityTier: formValueOr(r, "tier", "concept"),
		CandidateCount: candidates, WorkflowVersionID: versionID, RequestJSON: string(raw),
		Enqueue: formValueOr(r, "execution", "local") != "campaign",
	}, user.ID)
	if err != nil {
		s.log.Error("create request", "err", err)
		http.Redirect(w, r, s.videoBase(r)+"/produce?msg="+urlMsg("Could not create request.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "generation.request", "generation_request", req.ID, "created generation request", "{}")
	if job != nil {
		http.Redirect(w, r, s.videoBase(r)+"/jobs/"+job.ID, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.videoBase(r)+"/compute?msg="+urlMsg("Request saved for campaign planning.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	_, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "produce"
	jobID := chiURLParam(r, "jobID")
	job, err := s.store.GetJob(r.Context(), jobID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	// Scope: the job's request must belong to this video.
	req, err := s.store.GetRequest(r.Context(), job.AggregateID)
	if err != nil || req.VideoID != video.ID {
		s.notFound(w, r)
		return
	}
	data := templates.JobPageData{Shell: shell, Job: templates.JobView{ID: job.ID, Type: job.JobType, State: job.State, AggregateID: job.AggregateID, CreatedAt: job.CreatedAt}}
	events, _ := s.store.ListJobEvents(r.Context(), job.ID)
	for _, e := range events {
		data.Events = append(data.Events, templates.JobEventView{Sequence: e.Sequence, Phase: e.Phase, Level: e.Level, Message: e.Message, Progress: e.Progress, CreatedAt: e.CreatedAt})
	}
	attempts, _ := s.store.ListAttempts(r.Context(), req.ID)
	for _, a := range attempts {
		av := templates.AttemptView{Number: a.AttemptNumber, Status: a.Status}
		if a.Seed.Valid {
			av.Seed = fmt.Sprintf("%d", a.Seed.Int64)
		}
		if a.ModelID.Valid {
			av.Model = a.ModelID.String
		}
		if a.DurationMs.Valid {
			av.DurationMs = fmt.Sprintf("%d ms", a.DurationMs.Int64)
		}
		if a.ErrorMessage.Valid {
			av.Error = a.ErrorMessage.String
		}
		data.Attempts = append(data.Attempts, av)
	}
	assets, _ := s.store.ListAssetsForRequest(r.Context(), req.ID)
	data.Assets = s.assetViews(r.Context(), assets)
	s.renderPage(w, r, templates.JobPage(data))
}

func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	_, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	jobID := chiURLParam(r, "jobID")
	job, err := s.store.GetJob(r.Context(), jobID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	req, err := s.store.GetRequest(r.Context(), job.AggregateID)
	if err != nil || req.VideoID != video.ID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	events, _ := s.store.ListJobEvents(r.Context(), job.ID)
	data := templates.JobPageData{Shell: shell, Job: templates.JobView{ID: job.ID, Type: job.JobType, State: job.State, AggregateID: job.AggregateID, CreatedAt: job.CreatedAt}}
	for _, e := range events {
		data.Events = append(data.Events, templates.JobEventView{Sequence: e.Sequence, Phase: e.Phase, Level: e.Level, Message: e.Message, Progress: e.Progress, CreatedAt: e.CreatedAt})
	}
	s.renderComponent(w, r, templates.JobStatusFragment(data))
}

func (s *Server) assetViews(ctx context.Context, assets []store.Asset) []templates.AssetView {
	out := make([]templates.AssetView, 0, len(assets))
	for _, a := range assets {
		av := templates.AssetView{ID: a.ID, Kind: a.Kind, Role: a.Role, MimeType: a.MimeType, URL: "/assets/" + a.ID, Checksum: a.Checksum, SizeBytes: a.SizeBytes, CreatedAt: a.CreatedAt}
		if att, err := s.store.GetAttemptByAsset(ctx, a.ID); err == nil && att != nil {
			if att.Seed.Valid {
				av.Seed = fmt.Sprintf("%d", att.Seed.Int64)
			}
			if att.ModelID.Valid {
				av.Model = att.ModelID.String
			}
		}
		out = append(out, av)
	}
	return out
}

// handleAssetFile serves a stored asset after workspace-scope authorization.
func (s *Server) handleAssetFile(w http.ResponseWriter, r *http.Request) {
	user := UserFrom(r.Context())
	assetID := chiURLParam(r, "assetID")
	asset, err := s.store.GetAsset(r.Context(), assetID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.store.RoleFor(r.Context(), asset.WorkspaceID, user.ID); err != nil && !user.IsSystemAdmin {
		s.forbidden(w, r)
		return
	}
	f, err := s.media.Open(asset.StorageURI)
	if err != nil {
		s.notFound(w, r)
		return
	}
	defer f.Close()
	if asset.MimeType != "" {
		w.Header().Set("Content-Type", asset.MimeType)
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if _, err := io.Copy(w, f); err != nil {
		s.log.Error("serve asset", "err", err)
	}
}

func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "Workflows")
	shell.ActiveNav = "workflows"
	pkgs, _ := s.store.ListWorkflowPackages(r.Context())
	data := templates.WorkflowsData{Shell: shell}
	for _, p := range pkgs {
		wv := templates.WorkflowView{PackageID: p.ID, Name: p.Name, Slug: p.Slug, Task: p.Task, Source: p.Source}
		if p.LatestVersion.Valid {
			wv.VersionID = p.LatestVersion.String
			wv.VersionStatus = p.VersionStatus.String
			if bs, err := s.store.ListBindings(r.Context(), p.LatestVersion.String); err == nil {
				wv.Bindings = len(bs)
			}
		}
		data.Workflows = append(data.Workflows, wv)
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	_ = ws
	s.renderPage(w, r, templates.WorkflowsPage(data))
}
