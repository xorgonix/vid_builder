// Package planner turns project context into structured, human-reviewable
// proposals using the configured LM Studio model (spec §12.5, §15.2, §15.3).
package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vidbuild/internal/blocking"
	"vidbuild/internal/llm"
)

// Planner wraps the LLM client with proposal-oriented prompts.
type Planner struct {
	client *llm.Client
}

// New constructs a Planner.
func New(client *llm.Client) *Planner { return &Planner{client: client} }

// Enabled reports whether the underlying LLM is configured.
func (p *Planner) Enabled() bool { return p != nil && p.client != nil }

// Health checks the configured LM Studio server and model.
func (p *Planner) Health(ctx context.Context) error {
	if !p.Enabled() {
		return fmt.Errorf("planner: LLM disabled")
	}
	return p.client.Ping(ctx)
}

// Reply asks the planner to respond in conversation (no mutation).
func (p *Planner) Reply(ctx context.Context, projectContext string, history []llm.Message) (string, error) {
	if !p.Enabled() {
		return "", fmt.Errorf("planner: LLM disabled")
	}
	system := "You are the vid-build planner. Discuss the creator's video idea helpfully and concretely. " +
		"You propose; you never decide and you never claim to have changed project data. " +
		"Keep replies concise and ask at most one clarifying question at the end."
	if projectContext != "" {
		system += "\n\nProject context:\n" + projectContext
	}
	return p.client.Chat(ctx, system, history)
}

// BriefPayload is the structured shape of a project brief proposal.
type BriefPayload struct {
	Premise         string `json:"premise"`
	Theme           string `json:"theme"`
	Audience        string `json:"audience"`
	DesiredReaction string `json:"desired_reaction"`
	Pace            string `json:"pace"`
	Excitement      string `json:"excitement"`
	MusicIntent     string `json:"music_intent"`
	SoundIntent     string `json:"sound_intent"`
	VisualStyle     string `json:"visual_style"`
	CameraLanguage  string `json:"camera_language"`
	ProductionNotes string `json:"production_notes"`
	Summary         string `json:"summary"`
}

const briefSchema = `{
  "premise": "one paragraph statement of the video's core idea",
  "theme": "thematic through-line",
  "audience": "who this is for",
  "desired_reaction": "what the audience should feel or do",
  "pace": "slow|moderate|fast and character of pacing",
  "excitement": "low|medium|high",
  "music_intent": "role of music",
  "sound_intent": "role of sound design",
  "visual_style": "look, palette, references",
  "camera_language": "movement and framing tendencies",
  "production_notes": "constraints, risks, continuity notes",
  "summary": "one-line summary of the proposal"
}`

// BriefProposal asks the model for a structured brief.
func (p *Planner) BriefProposal(ctx context.Context, projectContext, instruction string, history []llm.Message) (*BriefPayload, error) {
	if !p.Enabled() {
		return nil, fmt.Errorf("planner: LLM disabled")
	}
	system := "You are the vid-build planner. Produce a single structured creative brief proposal. " +
		"Base it only on the creator's stated intent and the project context. Do not invent client requirements."
	if projectContext != "" {
		system += "\n\nProject context:\n" + projectContext
	}
	if instruction != "" {
		system += "\n\nCreator instruction:\n" + instruction
	}
	raw, err := p.client.ChatJSON(ctx, system, history, briefSchema)
	if err != nil {
		return nil, err
	}
	var payload BriefPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// OutlinePayload is a proposed section/scene/shot structure.
type OutlinePayload struct {
	Summary  string         `json:"summary"`
	Sections []SectionDraft `json:"sections"`
}

// SectionDraft is a proposed section.
type SectionDraft struct {
	Title            string       `json:"title"`
	Purpose          string       `json:"purpose"`
	Summary          string       `json:"summary"`
	DurationBudgetMs int          `json:"duration_budget_ms"`
	Scenes           []SceneDraft `json:"scenes"`
}

// SceneDraft is a proposed scene.
type SceneDraft struct {
	Title            string      `json:"title"`
	Synopsis         string      `json:"synopsis"`
	Purpose          string      `json:"purpose"`
	Pace             string      `json:"pace"`
	AudienceReaction string      `json:"audience_reaction"`
	DurationBudgetMs int         `json:"duration_budget_ms"`
	Shots            []ShotDraft `json:"shots"`
}

// ShotDraft is a proposed shot.
type ShotDraft struct {
	Title            string `json:"title"`
	Purpose          string `json:"purpose"`
	Treatment        string `json:"treatment"`
	DurationTargetMs int    `json:"duration_target_ms"`
}

const outlineSchema = `{
  "summary": "one-line summary",
  "sections": [
    {
      "title": "section title",
      "purpose": "what this division accomplishes",
      "summary": "short summary",
      "duration_budget_ms": 0,
      "scenes": [
        {
          "title": "scene title",
          "synopsis": "what happens",
          "purpose": "dramatic/explanatory function",
          "pace": "slow|moderate|fast",
          "audience_reaction": "intended reaction",
          "duration_budget_ms": 0,
          "shots": [
            { "title": "shot title", "purpose": "what the shot accomplishes", "treatment": "how it is staged", "duration_target_ms": 0 }
          ]
        }
      ]
    }
  ]
}`

// OutlineProposal asks the model for a structured outline.
func (p *Planner) OutlineProposal(ctx context.Context, projectContext, briefText, instruction string, history []llm.Message) (*OutlinePayload, error) {
	if !p.Enabled() {
		return nil, fmt.Errorf("planner: LLM disabled")
	}
	system := "You are the vid-build planner. Produce a single structured video outline. " +
		"Every shot must state a purpose and a visible change; never create purposeless filler. " +
		"Keep durations realistic and totals close to the target duration. " +
		"Use at most 3 sections, at most 2 scenes per section and at most 4 shots per scene. " +
		"Output compact JSON on a single line with no prose, no markdown and no trailing text."
	if projectContext != "" {
		system += "\n\nProject context:\n" + projectContext
	}
	if briefText != "" {
		system += "\n\nAccepted brief:\n" + briefText
	}
	if instruction != "" {
		system += "\n\nCreator instruction:\n" + instruction
	}
	raw, err := p.client.ChatJSON(ctx, system, history, outlineSchema)
	if err != nil {
		return nil, err
	}
	var payload OutlinePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if len(payload.Sections) == 0 {
		return nil, fmt.Errorf("planner: outline proposal contained no sections")
	}
	return &payload, nil
}

// BriefText renders a brief as plain text for prompt context.
func BriefText(brief map[string]string) string {
	var b strings.Builder
	for _, k := range []string{"premise", "theme", "audience", "desired_reaction", "pace", "excitement", "music_intent", "sound_intent", "visual_style", "camera_language", "production_notes"} {
		if v := brief[k]; v != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	return b.String()
}

// SpecSuggestion is a proposed shot specification.
type SpecSuggestion struct {
	Positive   string         `json:"positive_prompt"`
	Negative   string         `json:"negative_prompt"`
	Structured map[string]any `json:"structured_spec_json"`
	Continuity map[string]any `json:"continuity_json"`
	Summary    string         `json:"summary"`
}

const specSchema = `{
  "positive_prompt": "what should be present in the frame",
  "negative_prompt": "what must be avoided",
  "structured_spec_json": { "notes": "optional structured intent" },
  "continuity_json": { "notes": "optional continuity constraints" },
  "summary": "one-line summary"
}`

// SuggestSpec proposes a specification for one endpoint or the motion spec.
func (p *Planner) SuggestSpec(ctx context.Context, projectContext, kind, shotPurpose, treatment, current string) (*SpecSuggestion, error) {
	if !p.Enabled() {
		return nil, fmt.Errorf("planner: LLM disabled")
	}
	system := "You are the vid-build planner. Propose a single shot specification of kind '" + kind + "'. " +
		"For start/end frames, describe the visible state at that moment. For motion, describe subject, camera and environment movement and the negative motion. " +
		"Do not claim to have changed anything. Output only JSON."
	if projectContext != "" {
		system += "\n\nProject context:\n" + projectContext
	}
	system += "\n\nShot purpose: " + shotPurpose + "\nTreatment: " + treatment
	if current != "" {
		system += "\nCurrent specification (improve or replace):\n" + current
	}
	raw, err := p.client.ChatJSON(ctx, system, nil, specSchema)
	if err != nil {
		return nil, err
	}
	var out SpecSuggestion
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BlockingSuggestion is a proposed start/end blocking pair.
type BlockingSuggestion struct {
	Start   blocking.Spec `json:"start"`
	End     blocking.Spec `json:"end"`
	Summary string        `json:"summary"`
}

const blockingSchema = `{
  "summary": "one-line summary of the staged change",
  "start": {
    "camera": {"shot_size":"wide|medium|close|extreme_close","angle":"eye|low|high|dutch","x":0.05,"y":0.05,"w":0.9,"h":0.9},
    "actors": [{"name":"Subject","x":0.4,"y":0.55,"body_rotation":0,"pose":"standing","gaze_x":0.6,"gaze_y":0.4,"facing":"camera"}],
    "props": [{"name":"tool","x":0.7,"y":0.6}],
    "arrows": [{"kind":"movement","x1":0.4,"y1":0.55,"x2":0.6,"y2":0.55}]
  },
  "end": { "camera": {}, "actors": [], "props": [], "arrows": [] }
}`

// SuggestBlocking proposes structured blocking JSON for both endpoints.
func (p *Planner) SuggestBlocking(ctx context.Context, projectContext, shotPurpose, treatment, startPrompt, endPrompt string) (*BlockingSuggestion, error) {
	if !p.Enabled() {
		return nil, fmt.Errorf("planner: LLM disabled")
	}
	system := "You are the vid-build planner. Propose structured start and end blocking for one shot as JSON only. " +
		"All coordinates are normalized 0.0-1.0. The end state must visibly differ from the start state in pose, position, gaze, props or camera. " +
		"Do not include any prose outside the JSON."
	if projectContext != "" {
		system += "\n\nProject context:\n" + projectContext
	}
	system += "\n\nShot purpose: " + shotPurpose + "\nTreatment: " + treatment
	if startPrompt != "" {
		system += "\nStart frame intent: " + startPrompt
	}
	if endPrompt != "" {
		system += "\nEnd frame intent: " + endPrompt
	}
	raw, err := p.client.ChatJSON(ctx, system, nil, blockingSchema)
	if err != nil {
		return nil, err
	}
	var out BlockingSuggestion
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
