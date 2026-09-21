package web

import (
	"net/http"
	"strconv"

	"vidbuild/internal/store"
	"vidbuild/internal/templates"
	"vidbuild/internal/video"
)

// handleDurationVideo renders the prompt + 5/15/30s duration-video screen: the
// eligible shots, the workflow choices, and every planned segment with its own
// status, retry control and the assembled playable video.
func (s *Server) handleDurationVideo(w http.ResponseWriter, r *http.Request) {
	project, vid, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "duration"
	data := templates.DurationData{Shell: shell, Project: toProjectView(project), Video: toVideoView(vid)}

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
					dsv.Start = assetView(a)
					dsv.Endpoints = dsv.Endpoints || s.isApproved(r, a.ID)
				}
				if a, err := s.store.GetSelectedAsset(r.Context(), sh.ID, "end_frame"); err == nil {
					dsv.End = assetView(a)
					dsv.Endpoints = dsv.Endpoints && s.isApproved(r, a.ID)
				} else {
					dsv.Endpoints = false
				}
				data.Shots = append(data.Shots, dsv)
			}
		}
	}

	if versions, err := s.store.ListSelectableVersions(r.Context()); err == nil {
		for _, v := range versions {
			name := "Workflow"
			if p, err := s.store.GetWorkflowPackage(r.Context(), v.WorkflowPackageID); err == nil {
				name = p.Name
			}
			data.Workflows = append(data.Workflows, templates.WorkflowOption{ID: v.ID, Name: name, Version: v.SemanticVersion, Status: v.Status})
		}
	}

	assemblies, _ := s.store.ListAssembliesForVideo(r.Context(), vid.ID)
	for _, asm := range assemblies {
		av := templates.DurationAssemblyView{RequestID: asm.RequestID, ShotID: asm.ShotID, Status: asm.Status, Error: asm.Error}
		if shot, err := s.store.GetShot(r.Context(), asm.ShotID); err == nil {
			av.ShotTitle = shot.Title
		}
		if asm.AssetID.Valid {
			if a, err := s.store.GetAsset(r.Context(), asm.AssetID.String); err == nil {
				av.Video = assetView(a)
				av.HasVideo = true
			}
		}
		segs, _ := s.store.ListSegments(r.Context(), asm.RequestID)
		for _, sg := range segs {
			sv := templates.DurationSegmentView{ID: sg.ID, Index: sg.Index, Status: sg.Status, Error: sg.Error}
			if sg.AssetID.Valid {
				if a, err := s.store.GetAsset(r.Context(), sg.AssetID.String); err == nil {
					sv.Asset = assetView(a)
					sv.HasFile = true
				}
			}
			av.Segments = append(av.Segments, sv)
		}
		data.Assemblies = append(data.Assemblies, av)
	}

	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.DurationPage(data))
}

// handleCreateDurationVideo plans a duration video: it compiles the requested
// 5/15/30 seconds into fixed segments and queues one job per segment.
func (s *Server) handleCreateDurationVideo(w http.ResponseWriter, r *http.Request) {
	project, vid, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shotID := r.FormValue("shot_id")
	duration, _ := strconv.Atoi(r.FormValue("duration"))
	workflowID := r.FormValue("workflow_id")
	shot, err := s.store.GetShot(r.Context(), shotID)
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
		http.Redirect(w, r, s.videoBase(r)+"/duration?msg="+urlMsg("Select and approve both the start and end frames first.")+"&kind=error", http.StatusSeeOther)
		return
	}
	if _, err := video.Plan(duration); err != nil {
		http.Redirect(w, r, s.videoBase(r)+"/duration?msg="+urlMsg(err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}

	var specID string
	specVersion := 0
	if spec, err := s.store.GetCurrentSpec(r.Context(), shot.ID, "motion"); err == nil && spec != nil {
		specID, specVersion = spec.ID, spec.Version
	}

	ws := WorkspaceFrom(r.Context())
	user := UserFrom(r.Context())
	svc := &video.Service{Store: s.store}
	_, segs, err := svc.Plan(r.Context(), video.PlanInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: specID, SpecVersion: specVersion, DurationSeconds: duration,
		Positive: r.FormValue("positive"), Negative: r.FormValue("negative"),
		FPS: float64(vid.FPSNum), Width: vid.Width, Height: vid.Height,
		WorkflowVersionID: workflowID,
		StartAssetID:      startA.ID, EndAssetID: endA.ID,
	}, user.ID)
	if err != nil {
		s.log.Error("plan duration video", "err", err)
		http.Redirect(w, r, s.videoBase(r)+"/duration?msg="+urlMsg("Could not plan the duration video: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "video.duration", "generation_request", segs[0].RequestID, "planned duration video", "{}")
	http.Redirect(w, r, s.videoBase(r)+"/duration?msg="+urlMsg("Queued "+strconv.Itoa(len(segs))+" segments.")+"&kind=success", http.StatusSeeOther)
}

// handleRetrySegment regenerates exactly one failed segment.
func (s *Server) handleRetrySegment(w http.ResponseWriter, r *http.Request) {
	_, vid, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	segmentID := chiURLParam(r, "segmentID")
	seg, err := s.store.GetSegment(r.Context(), segmentID)
	if err != nil || seg.VideoID != vid.ID {
		s.notFound(w, r)
		return
	}
	svc := &video.Service{Store: s.store}
	if err := svc.RetrySegment(r.Context(), seg.ID); err != nil {
		http.Redirect(w, r, s.videoBase(r)+"/duration?msg="+urlMsg("Could not retry the segment: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.videoBase(r)+"/duration?msg="+urlMsg("Segment queued for retry.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) isApproved(r *http.Request, assetID string) bool {
	switch s.store.EffectiveApproval(r.Context(), assetID) {
	case "approve", "approve_with_exception":
		return true
	default:
		return false
	}
}

func assetView(a *store.Asset) templates.AssetView {
	if a == nil {
		return templates.AssetView{}
	}
	return templates.AssetView{
		ID: a.ID, Kind: a.Kind, Role: a.Role, MimeType: a.MimeType,
		URL: "/assets/" + a.ID, Checksum: a.Checksum, SizeBytes: a.SizeBytes, CreatedAt: a.CreatedAt,
	}
}
