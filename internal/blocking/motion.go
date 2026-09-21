package blocking

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MotionSpec is the structured motion document (spec §7.3). It supplements the
// motion prompt; it never replaces it.
type MotionSpec struct {
	SubjectMotions    []SubjectMotion `json:"subject_motions,omitempty"`
	CameraMotion      *CameraMotion   `json:"camera_motion,omitempty"`
	EnvironmentMotion []string        `json:"environment_motion,omitempty"`
	NegativeMotion    []string        `json:"negative_motion,omitempty"`
}

// SubjectMotion is one element's action over time.
type SubjectMotion struct {
	ElementID  string `json:"element_id"`
	Action     string `json:"action"`
	StartMs    int    `json:"start_ms"`
	DurationMs int    `json:"duration_ms"`
	Easing     string `json:"easing,omitempty"`
}

// CameraMotion is the camera move over time.
type CameraMotion struct {
	Kind       string `json:"kind"`
	Amount     string `json:"amount,omitempty"`
	StartMs    int    `json:"start_ms"`
	DurationMs int    `json:"duration_ms"`
}

var (
	easings = map[string]bool{
		"": true, "linear": true, "ease_in": true, "ease_out": true,
		"fast_out": true, "slow_in": true, "hold": true,
	}
	cameraKinds = map[string]bool{
		"": true, "static": true, "push_in": true, "pull_out": true,
		"pan_left": true, "pan_right": true, "tilt_up": true, "tilt_down": true,
		"orbit": true, "handheld": true, "crane": true,
	}
)

// Validate checks the structured motion document (spec §7.3, §13.3).
func (m MotionSpec) Validate() error {
	var issues []string
	seen := map[string]bool{}
	for i, sm := range m.SubjectMotions {
		if strings.TrimSpace(sm.ElementID) == "" {
			issues = append(issues, fmt.Sprintf("subject_motions[%d] needs an element_id", i))
		} else if seen[sm.ElementID] {
			issues = append(issues, fmt.Sprintf("subject_motions element_id %q is duplicated", sm.ElementID))
		} else {
			seen[sm.ElementID] = true
		}
		if strings.TrimSpace(sm.Action) == "" {
			issues = append(issues, fmt.Sprintf("subject_motions[%d] needs an action", i))
		}
		if sm.StartMs < 0 || sm.DurationMs < 0 {
			issues = append(issues, fmt.Sprintf("subject_motions[%d] timing cannot be negative", i))
		}
		if !easings[sm.Easing] {
			issues = append(issues, fmt.Sprintf("subject_motions[%d] easing %q is not a known easing", i, sm.Easing))
		}
	}
	if cm := m.CameraMotion; cm != nil {
		if !cameraKinds[cm.Kind] {
			issues = append(issues, fmt.Sprintf("camera_motion kind %q is not a known camera move", cm.Kind))
		}
		if cm.StartMs < 0 || cm.DurationMs < 0 {
			issues = append(issues, "camera_motion timing cannot be negative")
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("motion: %s", strings.Join(issues, "; "))
	}
	return nil
}

// ValidateMotionJSON decodes and validates a motion document.
func ValidateMotionJSON(raw string) error {
	if strings.TrimSpace(raw) == "" || raw == "{}" {
		return nil
	}
	var m MotionSpec
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return fmt.Errorf("motion: invalid JSON: %w", err)
	}
	return m.Validate()
}

// Marshal renders the motion document as compact JSON.
func (m MotionSpec) Marshal() string {
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
