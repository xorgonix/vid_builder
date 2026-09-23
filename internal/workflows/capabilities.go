package workflows

import (
	"encoding/json"
	"fmt"
)

// FrameRule describes a workflow's frame-count congruence (spec §5.5). For
// example Wan uses length ≡ 1 (mod 4).
type FrameRule struct {
	Type      string `json:"type"` // "modulo" (absence of a rule means any count)
	Modulus   int    `json:"modulus"`
	Remainder int    `json:"remainder"`
}

// FPS is a rational frame rate.
type FPS struct {
	Num int `json:"num"`
	Den int `json:"den"`
}

// VideoTiming is the workflow's timing capability (spec §5.5). Its presence is
// what allows a video Super Shot to be planned at all.
type VideoTiming struct {
	MinimumDurationMS          int       `json:"minimum_duration_ms"`
	MaximumDirectDurationMS    int       `json:"maximum_direct_duration_ms"`
	PreferredSegmentDurationMS int       `json:"preferred_segment_duration_ms"`
	MinimumFrames              int       `json:"minimum_frames"`
	MaximumFrames              int       `json:"maximum_frames"`
	FrameRule                  FrameRule `json:"frame_rule"`
	SupportedFPS               []FPS     `json:"supported_fps"`
}

// Inputs declares which terminal images the workflow accepts.
type Inputs struct {
	StartImage bool `json:"start_image"`
	EndImage   bool `json:"end_image"`
}

// Continuation declares how (if at all) the workflow consumes predecessor motion
// context. "anchor_only" means it does not.
type Continuation struct {
	Mode string `json:"mode"` // anchor_only | tail_frames | native_latent
}

// Capabilities is the versioned capability block stored on a workflow version's
// manifest. Observed benchmark results live separately; capability data is
// immutable so a later measurement cannot rewrite what an old request declared.
type Capabilities struct {
	VideoTiming      *VideoTiming `json:"video_timing,omitempty"`
	Inputs           Inputs       `json:"inputs"`
	Continuation     Continuation `json:"continuation"`
	EndpointPolicy   string       `json:"endpoint_policy,omitempty"` // conditioned | enforced
	Verified         bool         `json:"verified"`
	VerificationNote string       `json:"verification_note,omitempty"`
}

// SupportsEndpoints reports whether both terminal images are accepted.
func (c *Capabilities) SupportsEndpoints() bool {
	return c != nil && c.Inputs.StartImage && c.Inputs.EndImage
}

// SupportsContext reports whether the workflow consumes predecessor motion
// context (tail frames or native latent). Context is only usable when the graph
// also targets the supplied last image; callers must check both.
func (c *Capabilities) SupportsContext() bool {
	if c == nil {
		return false
	}
	return c.Continuation.Mode == "tail_frames" || c.Continuation.Mode == "native_latent"
}

// SupportsFPS reports whether num/den is a supported frame rate.
func (t *VideoTiming) SupportsFPS(num, den int) bool {
	if t == nil || den <= 0 {
		return false
	}
	for _, f := range t.SupportedFPS {
		if f.Num*den == num*f.Den && f.Num*den > 0 {
			return true
		}
	}
	return false
}

// ParseCapabilities reads the capability block from a stored manifest JSON.
// A manifest without capabilities returns (nil, nil): absence is not an error,
// but planning must treat it as "no timing capability".
func ParseCapabilities(manifestJSON string) (*Capabilities, error) {
	if manifestJSON == "" {
		return nil, nil
	}
	var wrapper struct {
		Capabilities *Capabilities `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &wrapper); err != nil {
		return nil, fmt.Errorf("workflows: parse capabilities: %w", err)
	}
	return wrapper.Capabilities, nil
}

// Validate performs a basic structural check of a capability block.
func (c *Capabilities) Validate() error {
	if c == nil {
		return fmt.Errorf("workflows: no capabilities declared")
	}
	switch c.Continuation.Mode {
	case "", "anchor_only", "tail_frames", "native_latent":
	default:
		return fmt.Errorf("workflows: unknown continuation mode %q", c.Continuation.Mode)
	}
	if c.VideoTiming != nil {
		t := c.VideoTiming
		if t.MinimumDurationMS < 0 || t.MaximumDirectDurationMS < 0 {
			return fmt.Errorf("workflows: negative timing bounds")
		}
		if t.MinimumFrames < 0 || (t.MaximumFrames > 0 && t.MaximumFrames < t.MinimumFrames) {
			return fmt.Errorf("workflows: invalid frame bounds")
		}
		if t.FrameRule.Type == "modulo" && t.FrameRule.Modulus <= 0 {
			return fmt.Errorf("workflows: invalid frame rule modulus")
		}
		for _, f := range t.SupportedFPS {
			if f.Num <= 0 || f.Den <= 0 {
				return fmt.Errorf("workflows: invalid supported fps %d/%d", f.Num, f.Den)
			}
		}
	}
	return nil
}
