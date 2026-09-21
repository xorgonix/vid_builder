package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"vidbuild/internal/blocking"
	"vidbuild/internal/llm"
	"vidbuild/internal/planner"
	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

var briefFields = []struct{ Key, Label string }{
	{"premise", "Premise"},
	{"theme", "Theme"},
	{"audience", "Audience"},
	{"desired_reaction", "Desired reaction"},
	{"pace", "Pace"},
	{"excitement", "Excitement"},
	{"music_intent", "Music intent"},
	{"sound_intent", "Sound intent"},
	{"visual_style", "Visual style"},
	{"camera_language", "Camera language"},
	{"production_notes", "Production notes"},
}

func (s *Server) handleDevelop(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "develop"
	data := templates.DevelopData{
		Shell: toShellPtr(shell), Project: toProjectView(project), Video: toVideoView(video),
		PlannerEnabled: s.planner.Enabled(),
	}
	if brief, err := s.store.GetCurrentBrief(r.Context(), project.ID); err == nil && brief != nil {
		data.Brief = &templates.BriefView{
			Version: brief.Version, Premise: brief.Premise, Theme: brief.Theme, Audience: brief.Audience,
			DesiredReaction: brief.DesiredReaction, Pace: brief.Pace, Excitement: brief.Excitement,
			MusicIntent: brief.MusicIntent, SoundIntent: brief.SoundIntent, VisualStyle: brief.VisualStyle,
			CameraLanguage: brief.CameraLanguage, ProductionNotes: brief.ProductionNotes,
		}
	}
	threads, _ := s.store.ListThreads(r.Context(), project.ID, video.ID)
	active := r.URL.Query().Get("thread")
	if active == "" && len(threads) > 0 {
		active = threads[0].ID
	}
	for _, t := range threads {
		msgs, _ := s.store.ListMessages(r.Context(), t.ID)
		data.Threads = append(data.Threads, templates.ThreadView{ID: t.ID, Title: t.Title, MessageCount: len(msgs)})
	}
	data.ActiveThread = active
	if active != "" {
		msgs, _ := s.store.ListMessages(r.Context(), active)
		for _, m := range msgs {
			mv := templates.MessageView{Role: m.Role, Content: m.Content, CreatedAt: m.CreatedAt}
			if m.ModelID.Valid {
				mv.ModelID = m.ModelID.String
			}
			data.Messages = append(data.Messages, mv)
		}
	}
	proposals, _ := s.store.ListProposals(r.Context(), project.ID, video.ID)
	for _, p := range proposals {
		data.Proposals = append(data.Proposals, templates.ProposalView{ID: p.ID, Type: p.ProposalType, Summary: p.Summary, Status: p.Status, CreatedAt: p.CreatedAt})
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Error = m
	}
	s.renderPage(w, r, templates.DevelopPage(data))
}

func toShellPtr(s templates.ShellData) templates.ShellData { return s }

func (s *Server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = "Discovery"
	}
	user := UserFrom(r.Context())
	t, err := s.store.CreateThread(r.Context(), project.ID, video.ID, user.ID, title)
	if err != nil {
		s.log.Error("create thread", "err", err)
	}
	target := s.developURL(r, "")
	if t != nil {
		target = s.developURL(r, t.ID)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	_, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	threadID := chiURLParam(r, "threadID")
	_ = r.ParseForm()
	content := strings.TrimSpace(r.FormValue("content"))
	if content != "" {
		user := UserFrom(r.Context())
		if _, err := s.store.AppendMessage(r.Context(), threadID, "creator", content, "", user.ID); err != nil {
			s.log.Error("append message", "err", err)
		}
	}
	http.Redirect(w, r, s.developURL(r, threadID), http.StatusSeeOther)
}

func (s *Server) handleAskPlanner(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	threadID := chiURLParam(r, "threadID")
	history := s.plannerHistory(r, project.ID, video.ID, threadID)
	ctxText := s.plannerContext(r, project, video)
	reply, err := s.planner.Reply(r.Context(), ctxText, history)
	if err != nil {
		s.log.Error("planner reply", "err", err)
		http.Redirect(w, r, withQuery(s.developURL(r, threadID), flashQuery("Planner unavailable: "+err.Error(), "error")), http.StatusSeeOther)
		return
	}
	_, _ = s.store.AppendMessage(r.Context(), threadID, "planner", reply, s.cfg.LLM.Model, "")
	http.Redirect(w, r, s.developURL(r, threadID), http.StatusSeeOther)
}

func (s *Server) handleProposeBrief(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	threadID := r.URL.Query().Get("thread")
	if threadID == "" {
		threads, _ := s.store.ListThreads(r.Context(), project.ID, video.ID)
		if len(threads) > 0 {
			threadID = threads[0].ID
		}
	}
	history := s.plannerHistory(r, project.ID, video.ID, threadID)
	payload, err := s.planner.BriefProposal(r.Context(), s.plannerContext(r, project, video), "", history)
	if err != nil {
		s.log.Error("brief proposal", "err", err)
		http.Redirect(w, r, withQuery(s.developURL(r, threadID), flashQuery("Brief proposal failed: "+err.Error(), "error")), http.StatusSeeOther)
		return
	}
	raw, _ := json.Marshal(payload)
	user := UserFrom(r.Context())
	p, err := s.store.CreateProposal(r.Context(), project.ID, video.ID, "project_brief", string(raw), payload.Summary, s.cfg.LLM.Model, user.ID, project.Revision)
	if err != nil {
		s.log.Error("create proposal", "err", err)
		s.notFound(w, r)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "proposal.create", "proposal", p.ID, "brief proposal", "{}")
	http.Redirect(w, r, s.proposalURL(r, p.ID), http.StatusSeeOther)
}

func (s *Server) handleProposeOutline(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	threadID := r.URL.Query().Get("thread")
	if threadID == "" {
		threads, _ := s.store.ListThreads(r.Context(), project.ID, video.ID)
		if len(threads) > 0 {
			threadID = threads[0].ID
		}
	}
	briefText := ""
	if brief, err := s.store.GetCurrentBrief(r.Context(), project.ID); err == nil && brief != nil {
		briefText = planner.BriefText(briefMap(brief))
	}
	history := s.plannerHistory(r, project.ID, video.ID, threadID)
	payload, err := s.planner.OutlineProposal(r.Context(), s.plannerContext(r, project, video), briefText, "", history)
	if err != nil {
		s.log.Error("outline proposal", "err", err)
		http.Redirect(w, r, withQuery(s.developURL(r, threadID), flashQuery("Outline proposal failed: "+err.Error(), "error")), http.StatusSeeOther)
		return
	}
	raw, _ := json.Marshal(payload)
	user := UserFrom(r.Context())
	p, err := s.store.CreateProposal(r.Context(), project.ID, video.ID, "video_outline", string(raw), payload.Summary, s.cfg.LLM.Model, user.ID, video.Revision)
	if err != nil {
		s.log.Error("create proposal", "err", err)
		s.notFound(w, r)
		return
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "proposal.create", "proposal", p.ID, "outline proposal", "{}")
	http.Redirect(w, r, s.proposalURL(r, p.ID), http.StatusSeeOther)
}

func (s *Server) handleProposalView(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "develop"
	proposal, err := s.store.GetProposal(r.Context(), chiURLParam(r, "proposalID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	data := templates.ProposalData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video),
		Proposal: templates.ProposalView{ID: proposal.ID, Type: proposal.ProposalType, Summary: proposal.Summary, Status: proposal.Status, CreatedAt: proposal.CreatedAt},
	}
	switch proposal.ProposalType {
	case "project_brief":
		var payload planner.BriefPayload
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		data.IsBrief = true
		data.Fields = []templates.FieldView{
			{Key: "premise", Label: "Premise", Value: payload.Premise, Checked: true},
			{Key: "theme", Label: "Theme", Value: payload.Theme, Checked: true},
			{Key: "audience", Label: "Audience", Value: payload.Audience, Checked: true},
			{Key: "desired_reaction", Label: "Desired reaction", Value: payload.DesiredReaction, Checked: true},
			{Key: "pace", Label: "Pace", Value: payload.Pace, Checked: true},
			{Key: "excitement", Label: "Excitement", Value: payload.Excitement, Checked: true},
			{Key: "music_intent", Label: "Music intent", Value: payload.MusicIntent, Checked: true},
			{Key: "sound_intent", Label: "Sound intent", Value: payload.SoundIntent, Checked: true},
			{Key: "visual_style", Label: "Visual style", Value: payload.VisualStyle, Checked: true},
			{Key: "camera_language", Label: "Camera language", Value: payload.CameraLanguage, Checked: true},
			{Key: "production_notes", Label: "Production notes", Value: payload.ProductionNotes, Checked: true},
		}
	case "video_outline":
		var payload planner.OutlinePayload
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		data.IsOutline = true
		data.Outline = outlineViewFromPayload(payload)
		data.OutlineJSON = proposal.PayloadJSON
	case "prompt_revision":
		var payload struct {
			ShotID     string `json:"shot_id"`
			Kind       string `json:"kind"`
			Positive   string `json:"positive"`
			Negative   string `json:"negative"`
			Structured string `json:"structured"`
		}
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		data.IsPrompt = true
		data.Fields = []templates.FieldView{
			{Key: "positive", Label: "Positive prompt", Value: payload.Positive, Checked: true},
			{Key: "negative", Label: "Negative prompt", Value: payload.Negative, Checked: true},
			{Key: "structured", Label: "Structured spec JSON", Value: payload.Structured, Checked: true},
		}
	case "blocking":
		var payload struct {
			ShotID  string        `json:"shot_id"`
			Start   blocking.Spec `json:"start"`
			End     blocking.Spec `json:"end"`
			Summary string        `json:"summary"`
		}
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		data.IsBlocking = true
		data.Blocking = &templates.BlockingPreview{
			ShotID:   payload.ShotID,
			Summary:  payload.Summary,
			StartSVG: blocking.RenderSVG(payload.Start),
			EndSVG:   blocking.RenderSVG(payload.End),
		}
	case "scene_plan":
		var payload struct {
			SceneID string `json:"scene_id"`
			*planner.BeatPayload
		}
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		if payload.BeatPayload == nil {
			payload.BeatPayload = &planner.BeatPayload{}
		}
		data.IsBeats = true
		data.SceneID = payload.SceneID
		for i, b := range payload.Beats {
			data.Beats = append(data.Beats, templates.BeatDraftView{
				Index: i, Title: b.Title, Purpose: b.Purpose, Trigger: b.Trigger, Action: b.Action,
				Reaction: b.Reaction, ResultingState: b.ResultingState, AudienceEffect: b.IntendedAudienceEffect,
				BudgetMs: b.DurationBudgetMs,
			})
		}
	case "shot_plan":
		var payload struct {
			BeatID string `json:"beat_id"`
			*planner.ShotBreakdownPayload
		}
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		if payload.ShotBreakdownPayload == nil {
			payload.ShotBreakdownPayload = &planner.ShotBreakdownPayload{}
		}
		data.IsShots = true
		data.BeatID = payload.BeatID
		for i, sh := range payload.Shots {
			data.Breakdown = append(data.Breakdown, templates.BreakdownShotView{
				Index: i, Title: sh.Title, NarrativePurpose: sh.NarrativePurpose, MustNotice: sh.MustNotice,
				StartState: sh.StartState, VisibleChange: sh.VisibleChange, EndState: sh.EndState,
				Framing: sh.Framing, DurationMs: sh.DurationTargetMs, TransitionIn: sh.TransitionIn,
				TransitionOut: sh.TransitionOut, Continuity: sh.Continuity, GenerationMethod: sh.GenerationMethod,
				Justification: sh.Justification,
			})
		}
	}
	s.renderPage(w, r, templates.ProposalPage(data))
}

func (s *Server) handleProposalApply(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	proposal, err := s.store.GetProposal(r.Context(), chiURLParam(r, "proposalID"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())

	switch proposal.ProposalType {
	case "project_brief":
		brief := store.Brief{
			ProjectID:        project.ID,
			SourceProposalID: nullString(proposal.ID),
		}
		for _, f := range briefFields {
			if r.FormValue("c_"+f.Key) == "" {
				continue
			}
			v := r.FormValue("f_" + f.Key)
			switch f.Key {
			case "premise":
				brief.Premise = v
			case "theme":
				brief.Theme = v
			case "audience":
				brief.Audience = v
			case "desired_reaction":
				brief.DesiredReaction = v
			case "pace":
				brief.Pace = v
			case "excitement":
				brief.Excitement = v
			case "music_intent":
				brief.MusicIntent = v
			case "sound_intent":
				brief.SoundIntent = v
			case "visual_style":
				brief.VisualStyle = v
			case "camera_language":
				brief.CameraLanguage = v
			case "production_notes":
				brief.ProductionNotes = v
			}
		}
		if _, err := s.store.InsertBrief(r.Context(), brief, user.ID); err != nil {
			s.log.Error("insert brief", "err", err)
			s.notFound(w, r)
			return
		}
		_ = s.store.DecideProposal(r.Context(), proposal.ID, "applied", user.ID)
		_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "brief.applied", "proposal", proposal.ID, "applied brief proposal", "{}")
		http.Redirect(w, r, withQuery(s.developURL(r, ""), flashQuery("Brief applied.", "success")), http.StatusSeeOther)
		return
	case "video_outline":
		var payload planner.OutlinePayload
		if err := json.Unmarshal([]byte(proposal.PayloadJSON), &payload); err != nil {
			s.notFound(w, r)
			return
		}
		if err := s.applyOutline(r, project, video, payload, user.ID); err != nil {
			s.log.Error("apply outline", "err", err)
			s.notFound(w, r)
			return
		}
		_ = s.store.DecideProposal(r.Context(), proposal.ID, "applied", user.ID)
		_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "outline.applied", "proposal", proposal.ID, "applied outline proposal", "{}")
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg("Outline applied.")+"&kind=success", http.StatusSeeOther)
		return
	case "prompt_revision":
		var payload struct {
			ShotID     string `json:"shot_id"`
			Kind       string `json:"kind"`
			Positive   string `json:"positive"`
			Negative   string `json:"negative"`
			Structured string `json:"structured"`
		}
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		kind := normalizeSpecKind(payload.Kind)
		if kind == "" {
			kind = "start_frame"
		}
		structured := strings.TrimSpace(r.FormValue("f_structured"))
		if structured == "" {
			structured = payload.Structured
		}
		if structured == "" {
			structured = "{}"
		}
		if !json.Valid([]byte(structured)) {
			structured = "{}"
		}
		if _, _, err := s.store.UpdateSpec(r.Context(), payload.ShotID, user.ID, kind,
			r.FormValue("f_positive"), r.FormValue("f_negative"), structured, "{}"); err != nil {
			s.log.Error("apply prompt revision", "err", err)
			s.notFound(w, r)
			return
		}
		_, _ = s.store.RecomputeShotReadiness(r.Context(), payload.ShotID)
		_ = s.store.DecideProposal(r.Context(), proposal.ID, "applied", user.ID)
		_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "spec.applied", "proposal", proposal.ID, "applied prompt revision", "{}")
		http.Redirect(w, r, withQuery(s.shotURL(r, payload.ShotID), flashQuery("Specification applied as a new version.", "success")), http.StatusSeeOther)
		return
	case "blocking":
		var payload struct {
			ShotID  string        `json:"shot_id"`
			Start   blocking.Spec `json:"start"`
			End     blocking.Spec `json:"end"`
			Summary string        `json:"summary"`
		}
		_ = json.Unmarshal([]byte(proposal.PayloadJSON), &payload)
		if r.FormValue("c_start") != "" {
			if spec, err := s.store.GetCurrentSpec(r.Context(), payload.ShotID, "start_frame"); err == nil {
				_, _ = s.store.SaveBlocking(r.Context(), spec.ID, "start", payload.Start.Marshal(), "model", s.cfg.LLM.Model, user.ID)
			}
		}
		if r.FormValue("c_end") != "" {
			if spec, err := s.store.GetCurrentSpec(r.Context(), payload.ShotID, "end_frame"); err == nil {
				_, _ = s.store.SaveBlocking(r.Context(), spec.ID, "end", payload.End.Marshal(), "model", s.cfg.LLM.Model, user.ID)
			}
		}
		_, _ = s.store.RecomputeShotReadiness(r.Context(), payload.ShotID)
		_ = s.store.DecideProposal(r.Context(), proposal.ID, "applied", user.ID)
		_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "blocking.applied", "proposal", proposal.ID, "applied blocking proposal", "{}")
		http.Redirect(w, r, withQuery(s.blockingURL(r, payload.ShotID), flashQuery("Blocking applied.", "success")), http.StatusSeeOther)
		return
	case "scene_plan":
		var payload struct {
			SceneID string `json:"scene_id"`
			*planner.BeatPayload
		}
		if err := json.Unmarshal([]byte(proposal.PayloadJSON), &payload); err != nil || payload.BeatPayload == nil || payload.SceneID == "" {
			s.notFound(w, r)
			return
		}
		if inVideo, _ := s.store.SceneInVideo(r.Context(), payload.SceneID, video.ID); !inVideo {
			s.notFound(w, r)
			return
		}
		applied := 0
		for i, b := range payload.Beats {
			if r.FormValue("c_beat_"+itoa(i)) == "" {
				continue
			}
			if _, err := s.store.CreateBeat(r.Context(), payload.SceneID,
				formValueOr(r, "f_beat_title_"+itoa(i), b.Title),
				formValueOr(r, "f_beat_purpose_"+itoa(i), b.Purpose),
				formValueOr(r, "f_beat_trigger_"+itoa(i), b.Trigger),
				formValueOr(r, "f_beat_action_"+itoa(i), b.Action),
				formValueOr(r, "f_beat_reaction_"+itoa(i), b.Reaction),
				formValueOr(r, "f_beat_state_"+itoa(i), b.ResultingState),
				formValueOr(r, "f_beat_effect_"+itoa(i), b.IntendedAudienceEffect),
				b.DurationBudgetMs); err != nil {
				s.log.Error("apply beat", "err", err)
				continue
			}
			applied++
		}
		_ = s.store.DecideProposal(r.Context(), proposal.ID, "applied", user.ID)
		_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "beats.applied", "proposal", proposal.ID, "applied beat proposal", "{}")
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg(itoa(applied)+" beat(s) applied.")+"&kind=success", http.StatusSeeOther)
		return
	case "shot_plan":
		var payload struct {
			BeatID string `json:"beat_id"`
			*planner.ShotBreakdownPayload
		}
		if err := json.Unmarshal([]byte(proposal.PayloadJSON), &payload); err != nil || payload.ShotBreakdownPayload == nil || payload.BeatID == "" {
			s.notFound(w, r)
			return
		}
		if inVideo, _ := s.store.BeatInVideo(r.Context(), payload.BeatID, video.ID); !inVideo {
			s.notFound(w, r)
			return
		}
		applied := 0
		for i, sh := range payload.Shots {
			if r.FormValue("c_shot_"+itoa(i)) == "" {
				continue
			}
			treatment := formValueOr(r, "f_shot_treatment_"+itoa(i), breakdownTreatment(sh))
			if _, err := s.store.CreateShotInBeat(r.Context(), payload.BeatID, user.ID,
				formValueOr(r, "f_shot_title_"+itoa(i), sh.Title),
				formValueOr(r, "f_shot_purpose_"+itoa(i), sh.NarrativePurpose),
				treatment, sh.DurationTargetMs); err != nil {
				s.log.Error("apply shot", "err", err)
				continue
			}
			applied++
		}
		_ = s.store.DecideProposal(r.Context(), proposal.ID, "applied", user.ID)
		_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "shots.applied", "proposal", proposal.ID, "applied shot-breakdown proposal", "{}")
		http.Redirect(w, r, s.outlineURL(r)+"?msg="+urlMsg(itoa(applied)+" shot(s) applied.")+"&kind=success", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.proposalURL(r, proposal.ID), http.StatusSeeOther)
}

// breakdownTreatment folds a proposed shot's production guidance into the
// shot's treatment text so nothing the planner explained is lost.
func breakdownTreatment(sh planner.BreakdownShot) string {
	parts := []string{}
	if sh.Framing != "" {
		parts = append(parts, "Framing: "+sh.Framing)
	}
	if sh.VisibleChange != "" {
		parts = append(parts, "Change: "+sh.VisibleChange)
	}
	if sh.GenerationMethod != "" {
		parts = append(parts, "Method: "+sh.GenerationMethod)
	}
	if sh.Justification != "" {
		parts = append(parts, "Why: "+sh.Justification)
	}
	return strings.Join(parts, "\n")
}

func (s *Server) applyOutline(r *http.Request, project *store.Project, video *store.Video, payload planner.OutlinePayload, userID string) error {
	for _, sec := range payload.Sections {
		created, err := s.store.CreateSection(r.Context(), video.ID, nil, sec.Title, sec.Purpose, sec.DurationBudgetMs)
		if err != nil {
			return err
		}
		for _, sc := range sec.Scenes {
			scene, err := s.store.CreateScene(r.Context(), created.ID, sc.Title, sc.Synopsis, sc.Purpose, sc.Pace, sc.AudienceReaction, sc.DurationBudgetMs)
			if err != nil {
				return err
			}
			for _, sh := range sc.Shots {
				if _, err := s.store.CreateShot(r.Context(), scene.ID, userID, sh.Title, sh.Purpose, sh.Treatment, sh.DurationTargetMs); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Server) handleProposalReject(w http.ResponseWriter, r *http.Request) {
	project, _, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	proposalID := chiURLParam(r, "proposalID")
	user := UserFrom(r.Context())
	_ = s.store.DecideProposal(r.Context(), proposalID, "rejected", user.ID)
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "proposal.rejected", "proposal", proposalID, "rejected proposal", "{}")
	http.Redirect(w, r, withQuery(s.developURL(r, ""), flashQuery("Proposal rejected.", "info")), http.StatusSeeOther)
}

func (s *Server) handleBriefRevise(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	user := UserFrom(r.Context())
	brief := store.Brief{ProjectID: project.ID}
	brief.Premise = strings.TrimSpace(r.FormValue("premise"))
	brief.Theme = strings.TrimSpace(r.FormValue("theme"))
	brief.Audience = strings.TrimSpace(r.FormValue("audience"))
	brief.DesiredReaction = strings.TrimSpace(r.FormValue("desired_reaction"))
	brief.Pace = strings.TrimSpace(r.FormValue("pace"))
	brief.Excitement = strings.TrimSpace(r.FormValue("excitement"))
	brief.MusicIntent = strings.TrimSpace(r.FormValue("music_intent"))
	brief.SoundIntent = strings.TrimSpace(r.FormValue("sound_intent"))
	brief.VisualStyle = strings.TrimSpace(r.FormValue("visual_style"))
	brief.CameraLanguage = strings.TrimSpace(r.FormValue("camera_language"))
	brief.ProductionNotes = strings.TrimSpace(r.FormValue("production_notes"))
	if _, err := s.store.InsertBrief(r.Context(), brief, user.ID); err != nil {
		s.log.Error("revise brief", "err", err)
	}
	_ = s.store.Audit(r.Context(), project.WorkspaceID, user.ID, "brief.revised", "project", project.ID, "manual brief revision", "{}")
	_ = video
	http.Redirect(w, r, withQuery(s.developURL(r, ""), flashQuery("Brief revised.", "success")), http.StatusSeeOther)
}

func (s *Server) handleBriefHistory(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "develop"
	history, _ := s.store.ListBriefHistory(r.Context(), project.ID)
	var versions []templates.BriefView
	for _, b := range history {
		versions = append(versions, templates.BriefView{
			Version: b.Version, Premise: b.Premise, Theme: b.Theme, Audience: b.Audience,
			DesiredReaction: b.DesiredReaction, Pace: b.Pace, Excitement: b.Excitement,
			MusicIntent: b.MusicIntent, SoundIntent: b.SoundIntent, VisualStyle: b.VisualStyle,
			CameraLanguage: b.CameraLanguage, ProductionNotes: b.ProductionNotes,
		})
	}
	s.renderPage(w, r, templates.BriefHistoryPage(templates.BriefHistoryData{
		Shell: shell, Project: toProjectView(project), Video: toVideoView(video), Versions: versions,
	}))
}

// helpers

func (s *Server) plannerHistory(r *http.Request, projectID, videoID, threadID string) []llm.Message {
	if threadID == "" {
		return nil
	}
	msgs, _ := s.store.ListMessages(r.Context(), threadID)
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		role := "user"
		switch m.Role {
		case "planner":
			role = "assistant"
		case "system":
			role = "system"
		}
		out = append(out, llm.Message{Role: role, Content: m.Content})
	}
	return out
}

func (s *Server) plannerContext(r *http.Request, project *store.Project, video *store.Video) string {
	var b strings.Builder
	b.WriteString("Project: " + project.Title + "\n")
	if project.Description != "" {
		b.WriteString("Description: " + project.Description + "\n")
	}
	b.WriteString("Video: " + video.Title + "\n")
	if video.Purpose != "" {
		b.WriteString("Purpose: " + video.Purpose + "\n")
	}
	b.WriteString("Format: " + video.Format + "\n")
	if video.TargetDurationMs > 0 {
		b.WriteString("Target duration: " + itoa(video.TargetDurationMs/1000) + "s\n")
	}
	if brief, err := s.store.GetCurrentBrief(r.Context(), project.ID); err == nil && brief != nil {
		b.WriteString("\nCurrent brief:\n" + planner.BriefText(briefMap(brief)))
	}
	return b.String()
}

func briefMap(b *store.Brief) map[string]string {
	return map[string]string{
		"premise": b.Premise, "theme": b.Theme, "audience": b.Audience,
		"desired_reaction": b.DesiredReaction, "pace": b.Pace, "excitement": b.Excitement,
		"music_intent": b.MusicIntent, "sound_intent": b.SoundIntent, "visual_style": b.VisualStyle,
		"camera_language": b.CameraLanguage, "production_notes": b.ProductionNotes,
	}
}

func oliveft(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (s *Server) developURL(r *http.Request, threadID string) string {
	u := s.videoBase(r) + "/develop"
	if threadID != "" {
		u += "?thread=" + threadID
	}
	return u
}

func (s *Server) outlineURL(r *http.Request) string { return s.videoBase(r) + "/outline" }

func (s *Server) proposalURL(r *http.Request, id string) string {
	return s.videoBase(r) + "/proposals/" + id
}

func outlineViewFromPayload(p planner.OutlinePayload) *templates.OutlineView {
	v := &templates.OutlineView{Summary: p.Summary}
	for _, sec := range p.Sections {
		sn := templates.SectionNode{Title: sec.Title, Purpose: sec.Purpose, BudgetMs: sec.DurationBudgetMs}
		for _, sc := range sec.Scenes {
			scn := templates.SceneNode{Title: sc.Title, Synopsis: sc.Synopsis, Purpose: sc.Purpose, Pace: sc.Pace, Reaction: sc.AudienceReaction, BudgetMs: sc.DurationBudgetMs}
			for _, sh := range sc.Shots {
				scn.Shots = append(scn.Shots, templates.ShotNode{Title: sh.Title, Purpose: sh.Purpose, Treatment: sh.Treatment, DurationMs: sh.DurationTargetMs})
			}
			sn.Scenes = append(sn.Scenes, scn)
		}
		v.Sections = append(v.Sections, sn)
	}
	return v
}

func (s *Server) videoBase(r *http.Request) string {
	ws := WorkspaceFrom(r.Context())
	return "/w/" + ws.ID + "/projects/" + chiURLParam(r, "p") + "/videos/" + chiURLParam(r, "v")
}
