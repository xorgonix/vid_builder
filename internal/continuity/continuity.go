// Package continuity defines the semantic shot state document and the
// structured transition/handoff between sequential shots (v0.02 spec §6.4, §6.5).
// Stable project-bible IDs are used wherever an entity exists.
package continuity

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Required transition types (spec §6.4).
const (
	TransitionContinuousAction = "continuous_action"
	TransitionHardCut          = "hard_cut"
	TransitionReactionCut      = "reaction_cut"
	TransitionPointOfViewCut   = "point_of_view_cut"
	TransitionCutaway          = "cutaway"
	TransitionMatchAction      = "match_action"
	TransitionMatchComposition = "match_composition"
	TransitionDissolve         = "dissolve"
	TransitionTimeJump         = "time_jump"
	TransitionLocationChange   = "location_change"
	TransitionParallelAction   = "parallel_action"
	TransitionAudioLedCut      = "audio_led_cut"
)

// TransitionTypes is the ordered list of valid transition types.
var TransitionTypes = []string{
	TransitionContinuousAction, TransitionHardCut, TransitionReactionCut,
	TransitionPointOfViewCut, TransitionCutaway, TransitionMatchAction,
	TransitionMatchComposition, TransitionDissolve, TransitionTimeJump,
	TransitionLocationChange, TransitionParallelAction, TransitionAudioLedCut,
}

// ShotSizes and angles used by the recompose block.
var (
	recomposeShotSizes = map[string]bool{"wide": true, "medium": true, "close": true, "extreme_close": true}
	recomposeAngles    = map[string]bool{"same": true, "reverse": true, "new": true, "eye": true, "low": true, "high": true, "dutch": true}
	cameraSides        = map[string]bool{"preserve_screen_direction": true, "flip": true, "neutral": true}
)

// Handoff is the structured transition between one shot and the next.
type Handoff struct {
	TransitionType string       `json:"transition_type"`
	DurationMs     int          `json:"duration_ms"`
	Motivation     string       `json:"motivation"`
	Inherit        InheritFlags `json:"inherit"`
	Recompose      Recompose    `json:"recompose"`
	Notes          string       `json:"notes"`
}

// InheritFlags names the facts carried from the previous shot's end state.
type InheritFlags struct {
	CharacterIdentity bool `json:"character_identity"`
	Costume           bool `json:"costume"`
	PhysicalCondition bool `json:"physical_condition"`
	EmotionalState    bool `json:"emotional_state"`
	KnowledgeState    bool `json:"knowledge_state"`
	PropState         bool `json:"prop_state"`
	PhysicalPositions bool `json:"physical_positions"`
	Location          bool `json:"location"`
	Lighting          bool `json:"lighting"`
	Weather           bool `json:"weather"`
}

// Recompose describes how the next shot is allowed to be reframed.
type Recompose struct {
	Allowed    bool   `json:"allowed"`
	ShotSize   string `json:"shot_size"`
	Angle      string `json:"angle"`
	CameraSide string `json:"camera_side"`
}

// IsValidTransitionType reports whether t is one of the required types.
func IsValidTransitionType(t string) bool {
	for _, v := range TransitionTypes {
		if v == t {
			return true
		}
	}
	return false
}

// Validate checks the handoff document (spec §6.4).
func (h Handoff) Validate() error {
	var issues []string
	if h.TransitionType == "" {
		issues = append(issues, "transition_type is required")
	} else if !IsValidTransitionType(h.TransitionType) {
		issues = append(issues, "transition_type "+h.TransitionType+" is not a known transition")
	}
	if h.DurationMs < 0 {
		issues = append(issues, "duration_ms cannot be negative")
	}
	if h.Recompose.ShotSize != "" && !recomposeShotSizes[h.Recompose.ShotSize] {
		issues = append(issues, "recompose.shot_size is not wide, medium, close or extreme_close")
	}
	if h.Recompose.Angle != "" && !recomposeAngles[h.Recompose.Angle] {
		issues = append(issues, "recompose.angle is not same, reverse, new, eye, low, high or dutch")
	}
	if h.Recompose.CameraSide != "" && !cameraSides[h.Recompose.CameraSide] {
		issues = append(issues, "recompose.camera_side is not preserve_screen_direction, flip or neutral")
	}
	if len(issues) > 0 {
		return fmt.Errorf("continuity: %s", strings.Join(issues, "; "))
	}
	return nil
}

// ValidateJSON decodes a handoff and validates it.
func ValidateJSON(raw string) error {
	var h Handoff
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		return fmt.Errorf("continuity: invalid handoff JSON: %w", err)
	}
	return h.Validate()
}

// Marshal renders the handoff as compact JSON.
func (h Handoff) Marshal() string {
	b, err := json.Marshal(h)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// StateEntity is one character or prop with an optional stable bible id.
type StateEntity struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
}

// State is the semantic state document for one shot endpoint (spec §6.5).
type State struct {
	Characters    []StateEntity `json:"characters,omitempty"`
	Props         []StateEntity `json:"props,omitempty"`
	Setting       string        `json:"setting,omitempty"`
	Time          string        `json:"time,omitempty"`
	Weather       string        `json:"weather,omitempty"`
	Emotion       string        `json:"emotion,omitempty"`
	Knowledge     []string      `json:"knowledge,omitempty"`
	OpenQuestions []string      `json:"open_questions,omitempty"`
}

// Validate checks the state document. Entities should carry a stable id when
// they exist in the project bible.
func (s State) Validate() error {
	var issues []string
	for i, c := range s.Characters {
		if strings.TrimSpace(c.ID) == "" && strings.TrimSpace(c.Name) == "" {
			issues = append(issues, fmt.Sprintf("characters[%d] needs an id or a name", i))
		}
	}
	for i, p := range s.Props {
		if strings.TrimSpace(p.ID) == "" && strings.TrimSpace(p.Name) == "" {
			issues = append(issues, fmt.Sprintf("props[%d] needs an id or a name", i))
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("continuity: %s", strings.Join(issues, "; "))
	}
	return nil
}

// ValidateStateJSON decodes a state document and validates it.
func ValidateStateJSON(raw string) error {
	var s State
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return fmt.Errorf("continuity: invalid state JSON: %w", err)
	}
	return s.Validate()
}

// Marshal renders the state as compact JSON.
func (s State) Marshal() string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Derivation is the result of carrying Shot A's end state into Shot B.
type Derivation struct {
	Start      State    `json:"start"`
	Inherited  []string `json:"inherited"`
	Changed    []string `json:"changed"`
	Unresolved []string `json:"unresolved"`
}

// DeriveNextStart proposes Shot B's start state from Shot A's end state, the
// transition handoff and Shot B's purpose. It distinguishes what is inherited,
// what is intentionally changed and what remains unresolved (spec §6.4, §8.5).
func DeriveNextStart(end State, h Handoff, nextPurpose string, invariants []string) Derivation {
	d := Derivation{}
	f := h.Inherit

	if f.CharacterIdentity || f.Costume || f.PhysicalCondition || f.EmotionalState || f.KnowledgeState {
		for _, c := range end.Characters {
			state := []string{}
			if f.CharacterIdentity && c.State != "" {
				state = append(state, c.State)
			}
			if f.EmotionalState && end.Emotion != "" {
				state = append(state, end.Emotion)
			}
			d.Start.Characters = append(d.Start.Characters, StateEntity{ID: c.ID, Name: c.Name, State: strings.Join(state, "; ")})
			d.Inherited = append(d.Inherited, "character identity: "+entityLabel(c))
		}
	}
	if f.PropState {
		d.Start.Props = append(d.Start.Props, end.Props...)
		for _, p := range end.Props {
			d.Inherited = append(d.Inherited, "prop state: "+entityLabel(p))
		}
	}
	if f.Location && end.Setting != "" {
		d.Start.Setting = end.Setting
		d.Inherited = append(d.Inherited, "setting: "+end.Setting)
	}
	if end.Time != "" {
		d.Start.Time = end.Time
		d.Inherited = append(d.Inherited, "time: "+end.Time)
	}
	if f.Weather && end.Weather != "" {
		d.Start.Weather = end.Weather
		d.Inherited = append(d.Inherited, "weather: "+end.Weather)
	}
	if f.EmotionalState && end.Emotion != "" {
		d.Start.Emotion = end.Emotion
	}
	if f.KnowledgeState {
		d.Start.Knowledge = append(d.Start.Knowledge, end.Knowledge...)
		for _, k := range end.Knowledge {
			d.Inherited = append(d.Inherited, "knowledge: "+k)
		}
	}

	if h.Recompose.Allowed {
		parts := []string{}
		if h.Recompose.ShotSize != "" {
			parts = append(parts, "shot size "+h.Recompose.ShotSize)
		}
		if h.Recompose.Angle != "" {
			parts = append(parts, "angle "+h.Recompose.Angle)
		}
		if h.Recompose.CameraSide != "" {
			parts = append(parts, "camera "+h.Recompose.CameraSide)
		}
		line := "recomposed"
		if len(parts) > 0 {
			line += ": " + strings.Join(parts, ", ")
		}
		d.Changed = append(d.Changed, line)
	}
	if !f.PhysicalPositions {
		d.Changed = append(d.Changed, "physical positions are not inherited; stage them for this shot")
	}
	if nextPurpose != "" {
		d.Changed = append(d.Changed, "shot purpose: "+nextPurpose)
	}
	for _, inv := range invariants {
		if strings.TrimSpace(inv) != "" {
			d.Inherited = append(d.Inherited, "bible invariant: "+inv)
		}
	}

	d.Unresolved = append(d.Unresolved, end.OpenQuestions...)
	if !f.CharacterIdentity && len(end.Characters) > 0 {
		d.Unresolved = append(d.Unresolved, "character identity was not marked inherited")
	}
	if !f.Location && end.Setting != "" {
		d.Unresolved = append(d.Unresolved, "location was not marked inherited: "+end.Setting)
	}
	if !f.KnowledgeState && len(end.Knowledge) > 0 {
		d.Unresolved = append(d.Unresolved, "knowledge state was not marked inherited")
	}
	return d
}

func entityLabel(e StateEntity) string {
	if e.Name != "" {
		return e.Name
	}
	return e.ID
}
