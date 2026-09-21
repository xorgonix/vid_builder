// Package animatic defines the playback settings for a blocking animatic
// (v0.02 spec §6.6, §11). Settings are stored as versioned JSON on the animatic.
package animatic

import (
	"encoding/json"
	"fmt"
)

// Settings are the animatic playback preferences.
type Settings struct {
	Speed             float64 `json:"speed"`
	Captions          bool    `json:"captions"`
	Voice             bool    `json:"voice"`
	DisplayMode       string  `json:"display_mode"`
	LoopMode          string  `json:"loop_mode"`
	TransitionPreview bool    `json:"transition_preview"`
	MetricsOverlay    bool    `json:"metrics_overlay"`
}

var (
	displayModes = map[string]bool{"blocking_only": true}
	loopModes    = map[string]bool{"none": true, "shot": true, "beat": true, "scene": true}
)

// Default returns sensible playback settings.
func Default() Settings {
	return Settings{Speed: 1, Captions: true, Voice: false, DisplayMode: "blocking_only", LoopMode: "none", TransitionPreview: true}
}

// Parse decodes settings, falling back to defaults on invalid input.
func Parse(raw string) Settings {
	s := Default()
	if raw == "" || raw == "{}" {
		return s
	}
	_ = json.Unmarshal([]byte(raw), &s)
	if s.Speed <= 0 {
		s.Speed = 1
	}
	if !displayModes[s.DisplayMode] {
		s.DisplayMode = "blocking_only"
	}
	if !loopModes[s.LoopMode] {
		s.LoopMode = "none"
	}
	return s
}

// Validate checks playback settings.
func (s Settings) Validate() error {
	if s.Speed < 0.25 || s.Speed > 4 {
		return fmt.Errorf("animatic: speed must be between 0.25 and 4")
	}
	if s.DisplayMode != "" && !displayModes[s.DisplayMode] {
		return fmt.Errorf("animatic: display_mode must be blocking_only")
	}
	if s.LoopMode != "" && !loopModes[s.LoopMode] {
		return fmt.Errorf("animatic: loop_mode must be none, shot, beat or scene")
	}
	return nil
}

// Marshal renders settings as compact JSON.
func (s Settings) Marshal() string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}
