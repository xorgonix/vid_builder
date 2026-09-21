package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"vidbuild/internal/blocking"
	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

// safeDraftMutations are the only mutation types that may be applied from an
// offline draft. Everything else requires a live server connection (spec §7.3).
var safeDraftMutations = map[string]bool{
	"concept_note":   true,
	"annotation":     true,
	"shot_text":      true,
	"blocking_draft": true,
}

type draftEnvelope struct {
	Drafts []draftItem `json:"drafts"`
}

type draftItem struct {
	ID           string          `json:"id"`
	MutationType string          `json:"mutation_type"`
	EntityID     string          `json:"entity_id"`
	BaseRevision int             `json:"base_revision"`
	CreatedAt    string          `json:"created_at"`
	Payload      json.RawMessage `json:"payload"`
}

type draftResult struct {
	DraftID string          `json:"draft_id"`
	Status  string          `json:"status"` // applied | conflict | rejected
	Reason  string          `json:"reason,omitempty"`
	Server  json.RawMessage `json:"server,omitempty"`
	Draft   json.RawMessage `json:"draft,omitempty"`
	Base    json.RawMessage `json:"base,omitempty"`
}

func (s *Server) handleDrafts(w http.ResponseWriter, r *http.Request) {
	project, video, shell, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	shell.ActiveNav = "more"
	data := templates.DraftsData{Shell: shell, Project: toProjectView(project), Video: toVideoView(video)}
	s.renderPage(w, r, templates.DraftsPage(data))
}

// handleDraftsSync applies a batch of offline drafts. It returns a three-way
// view (base / server / draft) for every conflict so nothing is overwritten
// silently (spec §7.1, §7.2).
func (s *Server) handleDraftsSync(w http.ResponseWriter, r *http.Request) {
	project, video, _, ok := s.loadVideoCtx(w, r)
	if !ok {
		return
	}
	var env draftEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid draft batch")
		return
	}
	user := UserFrom(r.Context())
	results := make([]draftResult, 0, len(env.Drafts))
	for _, d := range env.Drafts {
		results = append(results, s.applyDraft(r, project, video, d, user.ID))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
}

func (s *Server) applyDraft(r *http.Request, project *store.Project, video *store.Video, d draftItem, userID string) draftResult {
	res := draftResult{DraftID: d.ID, Draft: d.Payload}
	if !safeDraftMutations[d.MutationType] {
		res.Status = "rejected"
		res.Reason = "This change requires a live server connection."
		return res
	}
	switch d.MutationType {
	case "concept_note":
		var p struct {
			ThreadID string `json:"thread_id"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(d.Payload, &p); err != nil || strings.TrimSpace(p.Content) == "" || p.ThreadID == "" {
			res.Status = "rejected"
			res.Reason = "Draft is missing its thread or content."
			return res
		}
		if _, err := s.store.AppendMessage(r.Context(), p.ThreadID, "creator", strings.TrimSpace(p.Content), "", userID); err != nil {
			res.Status = "rejected"
			res.Reason = "Thread no longer exists."
			return res
		}
		res.Status = "applied"
		return res

	case "annotation":
		var p struct {
			ShotID string `json:"shot_id"`
			Body   string `json:"body"`
		}
		if err := json.Unmarshal(d.Payload, &p); err != nil || strings.TrimSpace(p.Body) == "" {
			res.Status = "rejected"
			res.Reason = "Annotation text is required."
			return res
		}
		if p.ShotID != "" {
			if inVideo, _ := s.store.ShotInVideo(r.Context(), p.ShotID, video.ID); !inVideo {
				res.Status = "rejected"
				res.Reason = "The shot is no longer in this video."
				return res
			}
		}
		if _, err := s.store.CreateAnnotation(r.Context(), project.WorkspaceID, project.ID, video.ID, p.ShotID, "", strings.TrimSpace(p.Body), userID); err != nil {
			res.Status = "rejected"
			res.Reason = "Could not save annotation."
			return res
		}
		res.Status = "applied"
		return res

	case "shot_text":
		var p struct {
			Title       string `json:"title"`
			Purpose     string `json:"purpose"`
			Treatment   string `json:"treatment"`
			DurationSec int    `json:"duration_s"`
		}
		if err := json.Unmarshal(d.Payload, &p); err != nil {
			res.Status = "rejected"
			res.Reason = "Draft payload is invalid."
			return res
		}
		shot, err := s.store.GetShot(r.Context(), d.EntityID)
		if err != nil {
			res.Status = "rejected"
			res.Reason = "The shot no longer exists."
			return res
		}
		if inVideo, _ := s.store.ShotInVideo(r.Context(), shot.ID, video.ID); !inVideo {
			res.Status = "rejected"
			res.Reason = "The shot is no longer in this video."
			return res
		}
		serverJSON, _ := json.Marshal(map[string]any{
			"title": shot.Title, "purpose": shot.Purpose, "treatment": shot.Treatment,
			"duration_s": shot.DurationTargetMs / 1000, "revision": shot.Revision,
		})
		res.Server = serverJSON
		if shot.Revision != d.BaseRevision {
			res.Status = "conflict"
			res.Reason = "The shot changed on the server since this draft was written."
			return res
		}
		if err := s.store.UpdateShot(r.Context(), shot.ID, p.Title, p.Purpose, p.Treatment, p.DurationSec*1000, d.BaseRevision); err != nil {
			res.Status = "conflict"
			res.Reason = "The shot changed on the server since this draft was written."
			return res
		}
		res.Status = "applied"
		return res

	case "blocking_draft":
		var p struct {
			Role     string `json:"role"`
			SpecJSON string `json:"spec_json"`
		}
		if err := json.Unmarshal(d.Payload, &p); err != nil {
			res.Status = "rejected"
			res.Reason = "Draft payload is invalid."
			return res
		}
		specKind := roleSpecKind(p.Role)
		if specKind == "" {
			res.Status = "rejected"
			res.Reason = "Unknown blocking role."
			return res
		}
		if inVideo, _ := s.store.ShotInVideo(r.Context(), d.EntityID, video.ID); !inVideo {
			res.Status = "rejected"
			res.Reason = "The shot is no longer in this video."
			return res
		}
		if err := blocking.ValidateJSON(p.SpecJSON); err != nil {
			res.Status = "rejected"
			res.Reason = err.Error()
			return res
		}
		doc := blocking.Parse(p.SpecJSON)
		doc.NormalizeV2()
		spec, err := s.store.GetCurrentSpec(r.Context(), d.EntityID, specKind)
		if err != nil {
			res.Status = "rejected"
			res.Reason = "The shot has no specification to attach blocking to."
			return res
		}
		if _, err := s.store.SaveBlocking(r.Context(), spec.ID, p.Role, doc.Marshal(), "human", "", userID); err != nil {
			res.Status = "rejected"
			res.Reason = "Could not save blocking."
			return res
		}
		res.Status = "applied"
		return res
	}
	res.Status = "rejected"
	res.Reason = "Unsupported draft type."
	return res
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
