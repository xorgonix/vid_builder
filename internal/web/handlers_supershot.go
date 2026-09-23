package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"vidbuild/internal/planner"
	"vidbuild/internal/store"
	"vidbuild/internal/templates"
	"vidbuild/internal/video"
)

// superShotPlanner adapts the LM Studio planner to video.Planner (contract C02).
type superShotPlanner struct{ p *planner.Planner }

func (a superShotPlanner) ProposeSuperShot(ctx context.Context, req video.SuperShotPromptRequest) (video.SuperShotSuggestion, error) {
	payload, err := a.p.SuperShotProposal(ctx, planner.SuperShotPromptInput{
		ShotPurpose: req.ShotPurpose, CompleteMovement: req.CompleteMovement,
		StartState: req.StartState, EndState: req.EndState,
		SegmentDurationsMS: req.SegmentDurationsMS, ContinuityLocks: req.ContinuityLocks,
	})
	if err != nil {
		return video.SuperShotSuggestion{}, err
	}
	states := make([]string, 0, len(payload.Anchors))
	for _, a := range payload.Anchors {
		states = append(states, a.State)
	}
	movements := make([]string, 0, len(payload.Segments))
	for _, seg := range payload.Segments {
		movements = append(movements, seg.Movement)
	}
	prov, _ := json.Marshal(map[string]any{"summary": payload.Summary, "continuity_locks": payload.ContinuityLocks, "source": "lmstudio"})
	return video.SuperShotSuggestion{IntermediateStates: states, SegmentMovements: movements, ProvenanceJSON: string(prov)}, nil
}

func ssAssetView(a *store.Asset) templates.AssetView {
	if a == nil {
		return templates.AssetView{}
	}
	return templates.AssetView{
		ID: a.ID, Kind: a.Kind, Role: a.Role, MimeType: a.MimeType,
		URL: "/assets/" + a.ID, Checksum: a.Checksum, SizeBytes: a.SizeBytes, CreatedAt: a.CreatedAt,
	}
}

// handleSuperShotList renders the video's Super Shot plans (also the target of
// the legacy /duration route, spec §8.1).
func (s *Server) handleSuperShotList(w http.ResponseWriter, r *http.Request) {
	project, vid, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "duration"
	data := templates.SuperShotListData{Shell: shell, Project: toProjectView(project), Video: toVideoView(vid)}
	plans, _ := s.store.ListSuperShotRequestsForVideo(r.Context(), vid.ID)
	for i := range plans {
		data.Plans = append(data.Plans, s.superShotView(r, &plans[i]))
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.SuperShotListPage(data))
}

// handleSuperShotNew renders the plan form.
func (s *Server) handleSuperShotNew(w http.ResponseWriter, r *http.Request) {
	project, vid, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "duration"
	data := templates.SuperShotFormData{Shell: shell, Project: toProjectView(project), Video: toVideoView(vid), PreselectShotID: r.URL.Query().Get("shot_id")}
	sections, _ := s.store.ListSections(r.Context(), vid.ID)
	for _, sec := range sections {
		scenes, _ := s.store.ListScenes(r.Context(), sec.ID)
		for _, sc := range scenes {
			shots, _ := s.store.ListShots(r.Context(), sc.ID)
			for _, sh := range shots {
				if sh.Status == "archived" {
					continue
				}
				dsv := templates.DurationShotView{ShotID: sh.ID, Title: sh.Title}
				if a, err := s.store.GetSelectedAsset(r.Context(), sh.ID, "start_frame"); err == nil {
					dsv.Start = ssAssetView(a)
					dsv.Endpoints = s.isApproved(r, a.ID)
				}
				if a, err := s.store.GetSelectedAsset(r.Context(), sh.ID, "end_frame"); err == nil {
					dsv.End = ssAssetView(a)
					dsv.Endpoints = dsv.Endpoints && s.isApproved(r, a.ID)
				} else {
					dsv.Endpoints = false
				}
				if sh.ID == data.PreselectShotID && sh.DurationTargetMs > 0 {
					data.DefaultDurationMS = sh.DurationTargetMs
				}
				data.Shots = append(data.Shots, dsv)
			}
		}
	}
	if data.DefaultDurationMS == 0 {
		data.DefaultDurationMS = 5000
	}
	if versions, err := s.store.ListSelectableVersions(r.Context()); err == nil {
		for _, v := range versions {
			name := "Workflow"
			task := ""
			if p, err := s.store.GetWorkflowPackage(r.Context(), v.WorkflowPackageID); err == nil {
				name = p.Name
				task = p.Task
			}
			opt := templates.WorkflowOption{ID: v.ID, Name: name, Version: v.SemanticVersion, Status: v.Status}
			if task == "image" {
				data.ImageWorkflows = append(data.ImageWorkflows, opt)
			} else {
				data.Workflows = append(data.Workflows, opt)
			}
		}
	}
	if profiles, err := s.store.ListModelProfiles(r.Context(), WorkspaceFrom(r.Context()).ID); err == nil {
		for _, p := range profiles {
			data.Profiles = append(data.Profiles, templates.ProfileOption{ID: p.ID, Name: p.Name, Checkpoint: p.ProfileCheckpoint()})
		}
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.SuperShotFormPage(data))
}

// handleSuperShotPlan validates inputs and persists a draft plan. It never
// enqueues generation (spec §4.8).
func (s *Server) handleSuperShotPlan(w http.ResponseWriter, r *http.Request) {
	project, vid, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shot, err := s.store.GetShot(r.Context(), r.FormValue("shot_id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if inVideo, _ := s.store.ShotInVideo(r.Context(), shot.ID, vid.ID); !inVideo {
		s.notFound(w, r)
		return
	}
	startA, errS := s.store.GetSelectedAsset(r.Context(), shot.ID, "start_frame")
	endA, errE := s.store.GetSelectedAsset(r.Context(), shot.ID, "end_frame")
	if errS != nil || errE != nil || !s.isApproved(r, startA.ID) || !s.isApproved(r, endA.ID) {
		http.Redirect(w, r, s.videoBase(r)+"/super-shots/new?msg="+urlMsg("Select and approve both the start and end images first.")+"&kind=error", http.StatusSeeOther)
		return
	}
	durationMS := int(math.Round(formFloat(r, "duration_s", 5) * 1000))
	fpsNum, fpsDen := vid.FPSNum, vid.FPSDen
	if fpsNum <= 0 {
		fpsNum, fpsDen = 8, 1
	}
	if fpsDen <= 0 {
		fpsDen = 1
	}
	var specID string
	specVersion := 0
	if spec, err := s.store.GetCurrentSpec(r.Context(), shot.ID, "motion"); err == nil && spec != nil {
		specID, specVersion = spec.ID, spec.Version
	}
	ws := WorkspaceFrom(r.Context())
	user := UserFrom(r.Context())
	svc := &video.Service{Store: s.store}
	if s.planner != nil && s.planner.Enabled() {
		svc.Planner = superShotPlanner{p: s.planner}
	}
	result, err := svc.PlanDraft(r.Context(), video.PlanRequest{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: specID, SpecVersion: specVersion,
		WorkflowVersionID: r.FormValue("workflow_id"), ModelProfileID: r.FormValue("model_profile_id"),
		ImageWorkflowVersionID: r.FormValue("image_workflow_id"), ImageModelProfileID: r.FormValue("image_profile_id"),
		RequestedDurationMS: durationMS, FPSNum: fpsNum, FPSDen: fpsDen, Width: vid.Width, Height: vid.Height,
		BasePositive: r.FormValue("positive"), BaseNegative: r.FormValue("negative"),
		ShotIntent: shot.Purpose, Camera: r.FormValue("camera"),
		StartState: r.FormValue("start_state"), EndState: r.FormValue("end_state"),
		StartAssetID: startA.ID, EndAssetID: endA.ID,
		IntermediateStates: splitLines(r.FormValue("intermediate_states")),
		ContinuityLocks:    splitCSV(r.FormValue("continuity")),
		PreferredStrategy:  r.FormValue("strategy"),
		ContextMode:        r.FormValue("context_mode"),
	}, user.ID)
	if err != nil {
		http.Redirect(w, r, s.videoBase(r)+"/super-shots/new?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "super_shot.plan", "super_shot_request", result.Plan.SuperShot.ID, "created Super Shot plan", "{}")
	http.Redirect(w, r, s.videoBase(r)+"/super-shots/"+result.Plan.SuperShot.ID, http.StatusSeeOther)
}

// handleSuperShotView renders the plan review page.
func (s *Server) handleSuperShotView(w http.ResponseWriter, r *http.Request) {
	project, vid, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	plan, ok2 := s.loadSuperShot(w, r, vid.ID)
	if !ok2 {
		return
	}
	shell.ActiveNav = "duration"
	data := templates.SuperShotPageData{Shell: shell, Project: toProjectView(project), Video: toVideoView(vid), SuperShot: s.superShotView(r, plan)}
	if assets, err := s.store.ListAssetsForVideo(r.Context(), vid.ID); err == nil {
		for i := range assets {
			a := assets[i]
			if strings.HasPrefix(a.MimeType, "image/") {
				data.Images = append(data.Images, ssAssetView(&a))
			}
		}
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.SuperShotPage(data))
}

// handleSuperShotApprove freezes the plan and queues initial work. The approval
// carries the revision the creator saw; a stale revision conflicts (contract C07,
// T12).
func (s *Server) handleSuperShotApprove(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	rev, _ := strconv.Atoi(r.FormValue("revision"))
	if rev <= 0 {
		rev = plan.Revision
	}
	svc := &video.Service{Store: s.store}
	if err := svc.Approve(r.Context(), plan.ID, rev); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, err.Error(), "error")
		return
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Plan approved; generation queued.", "success")
}

// handleSuperShotCancel stops future dispatch without deleting history.
func (s *Server) handleSuperShotCancel(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	_ = s.store.SetSuperShotStatus(r.Context(), plan.ID, "cancelled")
	s.superShotRedirect(w, r, vid, plan.ID, "Plan cancelled.", "success")
}

// handleSuperShotAnchorUpdate edits an intermediate anchor's state.
func (s *Server) handleSuperShotAnchorUpdate(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	anchor, err := s.store.GetAnchor(r.Context(), chiURLParam(r, "anchorID"))
	if err != nil || anchor.SuperShotRequestID != plan.ID {
		s.notFound(w, r)
		return
	}
	rev, _ := strconv.Atoi(r.FormValue("revision"))
	if err := s.store.UpdateAnchorState(r.Context(), anchor.ID, r.FormValue("state"), anchor.NegativePrompt, anchor.ContinuityJSON, rev); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, "Could not save the anchor state: "+err.Error(), "error")
		return
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Anchor state saved.", "success")
}

// handleSuperShotSegmentUpdate edits a segment's movement prompt.
func (s *Server) handleSuperShotSegmentUpdate(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	seg, err := s.store.GetSegment(r.Context(), chiURLParam(r, "segmentID"))
	if err != nil || !seg.SuperShotRequestID.Valid || seg.SuperShotRequestID.String != plan.ID {
		s.notFound(w, r)
		return
	}
	rev, _ := strconv.Atoi(r.FormValue("revision"))
	svc := &video.Service{Store: s.store}
	if err := svc.EditSegmentAction(r.Context(), seg.ID, r.FormValue("action"), rev); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, "Could not save the movement prompt: "+err.Error(), "error")
		return
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Movement prompt saved.", "success")
}

// handleSuperShotAnchorGenerate queues one intermediate anchor image.
func (s *Server) handleSuperShotAnchorGenerate(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	anchor, err := s.store.GetAnchor(r.Context(), chiURLParam(r, "anchorID"))
	if err != nil || anchor.SuperShotRequestID != plan.ID || anchor.Kind != "intermediate" {
		s.notFound(w, r)
		return
	}
	if err := s.enqueueAnchor(r, plan, anchor); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, err.Error(), "error")
		return
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Anchor queued.", "success")
}

// handleSuperShotGenerateMissing queues every intermediate anchor without an image.
func (s *Server) handleSuperShotGenerateMissing(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	anchors, _ := s.store.ListAnchors(r.Context(), plan.ID)
	n := 0
	for _, a := range anchors {
		if a.Kind == "intermediate" && !a.AssetID.Valid {
			if err := s.enqueueAnchor(r, plan, &a); err == nil {
				n++
			}
		}
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Queued "+strconv.Itoa(n)+" anchor(s).", "success")
}

// handleSuperShotSegmentRetry retries only the addressed segment.
func (s *Server) handleSuperShotSegmentRetry(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	seg, err := s.store.GetSegment(r.Context(), chiURLParam(r, "segmentID"))
	if err != nil || !seg.SuperShotRequestID.Valid || seg.SuperShotRequestID.String != plan.ID {
		s.notFound(w, r)
		return
	}
	svc := &video.Service{Store: s.store}
	if err := svc.RetrySegment(r.Context(), seg.ID); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, "Could not retry: "+err.Error(), "error")
		return
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Segment queued for retry.", "success")
}

// handleSuperShotAssemble idempotently assembles the chain when every segment is current.
func (s *Server) handleSuperShotAssemble(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	if !plan.GenerationRequestID.Valid {
		s.superShotRedirect(w, r, vid, plan.ID, "Plan has no generation request.", "error")
		return
	}
	svc := &video.Service{Store: s.store}
	if _, err := svc.TryAssemble(r.Context(), plan.GenerationRequestID.String); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, err.Error(), "error")
		return
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Assembly queued if every segment is ready.", "success")
}

// handleSuperShotAnchorAttach attaches a creator-supplied image to an
// intermediate anchor and invalidates the segments that depend on it (C03/C06).
func (s *Server) handleSuperShotAnchorAttach(w http.ResponseWriter, r *http.Request) {
	vid, plan, ok := s.loadSuperShotForAction(w, r)
	if !ok {
		return
	}
	anchor, err := s.store.GetAnchor(r.Context(), chiURLParam(r, "anchorID"))
	if err != nil || anchor.SuperShotRequestID != plan.ID || anchor.Kind != "intermediate" {
		s.notFound(w, r)
		return
	}
	asset, err := s.store.GetAsset(r.Context(), r.FormValue("asset_id"))
	if err != nil || asset.WorkspaceID != plan.WorkspaceID || asset.ProjectID != plan.ProjectID {
		s.superShotRedirect(w, r, vid, plan.ID, "That image is not available in this project.", "error")
		return
	}
	if !strings.HasPrefix(asset.MimeType, "image/") {
		s.superShotRedirect(w, r, vid, plan.ID, "Only image assets can be attached to an anchor.", "error")
		return
	}
	if err := s.store.SetAnchorAsset(r.Context(), anchor.ID, asset.ID); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, "Could not attach the image: "+err.Error(), "error")
		return
	}
	svc := &video.Service{Store: s.store}
	if err := svc.ChangeAnchor(r.Context(), anchor.ID); err != nil {
		s.superShotRedirect(w, r, vid, plan.ID, "Attached, but could not invalidate dependants: "+err.Error(), "error")
		return
	}
	s.superShotRedirect(w, r, vid, plan.ID, "Image attached; dependent segments marked stale.", "success")
}

func (s *Server) enqueueAnchor(r *http.Request, plan *store.SuperShotRequest, anchor *store.VideoAnchor) error {
	// Route through the service so the reset bumps the anchor revision and the job
	// key is attempt/revision-scoped (contract C05, C07).
	svc := &video.Service{Store: s.store}
	return svc.RetryAnchor(r.Context(), anchor.ID)
}

func (s *Server) superShotRedirect(w http.ResponseWriter, r *http.Request, vid *store.Video, planID, msg, kind string) {
	http.Redirect(w, r, s.videoBase(r)+"/super-shots/"+planID+"?msg="+urlMsg(msg)+"&kind="+kind, http.StatusSeeOther)
}

func (s *Server) loadSuperShot(w http.ResponseWriter, r *http.Request, videoID string) (*store.SuperShotRequest, bool) {
	plan, err := s.store.GetSuperShotRequest(r.Context(), chiURLParam(r, "id"))
	if err != nil || plan.VideoID != videoID {
		s.notFound(w, r)
		return nil, false
	}
	return plan, true
}

func (s *Server) loadSuperShotForAction(w http.ResponseWriter, r *http.Request) (*store.Video, *store.SuperShotRequest, bool) {
	_, vid, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return nil, nil, false
	}
	plan, ok2 := s.loadSuperShot(w, r, vid.ID)
	if !ok2 {
		return nil, nil, false
	}
	return vid, plan, true
}

// superShotView builds the display model for one plan.
func (s *Server) superShotView(r *http.Request, plan *store.SuperShotRequest) templates.SuperShotView {
	v := templates.SuperShotView{
		ID: plan.ID, ShotID: plan.ShotID, Strategy: plan.Strategy,
		RequestedDurationMS: plan.RequestedDurationMS, PlannedDurationMS: plan.PlannedDurationMS,
		Status: plan.Status, PlanVersion: plan.PlanVersion, Revision: plan.Revision, Summary: plan.PlanSummary,
		ContextMode: plan.ContextMode, LegacyInvalid: plan.LegacyInvalidPlan,
	}
	if shot, err := s.store.GetShot(r.Context(), plan.ShotID); err == nil {
		v.ShotTitle = shot.Title
	}
	anchors, _ := s.store.ListAnchors(r.Context(), plan.ID)
	for _, a := range anchors {
		av := templates.SuperShotAnchorView{
			ID: a.ID, Index: a.Index, TimeOffsetMS: a.TimeOffsetMS, Kind: a.Kind,
			State: a.StatePrompt, Status: a.Status, Error: a.Error, Revision: a.Revision,
			IsSupplied: a.Kind != "intermediate",
		}
		if a.AssetID.Valid {
			if asset, err := s.store.GetAsset(r.Context(), a.AssetID.String); err == nil {
				av.Asset = ssAssetView(asset)
				av.HasAsset = true
			}
		}
		v.Anchors = append(v.Anchors, av)
	}
	segments, _ := s.store.ListSegmentsForSuperShot(r.Context(), plan.ID)
	for _, seg := range segments {
		sv := templates.SuperShotSegmentView{
			ID: seg.ID, Index: seg.Index, Action: seg.ActionPrompt, DurationMS: seg.DurationMS,
			StartOffsetMS: seg.StartOffsetMS, EndOffsetMS: seg.EndOffsetMS, Frames: seg.FrameCount,
			Status: seg.Status, Stale: seg.Stale, Error: seg.Error, Revision: seg.Revision,
		}
		if seg.AssetID.Valid {
			if asset, err := s.store.GetAsset(r.Context(), seg.AssetID.String); err == nil {
				sv.Asset = ssAssetView(asset)
				sv.HasFile = true
			}
		}
		v.Segments = append(v.Segments, sv)
	}
	if plan.GenerationRequestID.Valid {
		if asm, err := s.store.GetAssembly(r.Context(), plan.GenerationRequestID.String); err == nil {
			if asm.AssetID.Valid {
				if asset, err := s.store.GetAsset(r.Context(), asm.AssetID.String); err == nil {
					v.Video = ssAssetView(asset)
					v.HasVideo = true
					v.ActualDurationMS = asm.ActualDurationMS
				}
			}
		}
	}
	return v
}

func (s *Server) isApproved(r *http.Request, assetID string) bool {
	switch s.store.EffectiveApproval(r.Context(), assetID) {
	case "approve", "approve_with_exception":
		return true
	default:
		return false
	}
}

func formFloat(r *http.Request, key string, def float64) float64 {	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func splitLines(v string) []string {
	var out []string
	for _, line := range strings.Split(v, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
