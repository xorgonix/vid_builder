// Package prompt implements layered prompt compilation (spec §9, §14.11A).
// Layers are model-independent authority; compilation produces the exact
// positive/negative fields that will be bound into a workflow.
package prompt

import (
	"encoding/json"
	"sort"
	"strings"
)

// Layer compilation modes. Locking is orthogonal to compilation and is not a
// mode (spec §9.2): a locked layer still contributes text.
const (
	ModeInherit  = "inherit"  // add inherited context
	ModeAppend   = "append"   // append to the compiled positive
	ModeReplace  = "replace"  // replace the compiled positive
	ModeSuppress = "suppress" // push the layer's positive text into the negative
)

// Layer is one editable prompt layer.
type Layer struct {
	Scope    string `json:"scope"`
	Mode     string `json:"mode"`
	Positive string `json:"positive"`
	Negative string `json:"negative"`
	Locked   bool   `json:"locked"`
	Priority int    `json:"priority"`
}

// UnmarshalJSON accepts v0.01 layer rows: the legacy "text" field is folded
// into Positive and the removed "lock" mode becomes locked=true + append, so a
// locked layer's text still contributes (spec §2.5, §9.2).
func (l *Layer) UnmarshalJSON(data []byte) error {
	var raw struct {
		Scope    string `json:"scope"`
		Mode     string `json:"mode"`
		Positive string `json:"positive"`
		Negative string `json:"negative"`
		Text     string `json:"text"`
		Locked   bool   `json:"locked"`
		Priority int    `json:"priority"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	l.Scope = raw.Scope
	l.Mode = raw.Mode
	l.Positive = raw.Positive
	if l.Positive == "" {
		l.Positive = raw.Text
	}
	l.Negative = raw.Negative
	l.Locked = raw.Locked
	l.Priority = raw.Priority
	l.Normalize()
	return nil
}

// Normalize maps legacy or unknown modes to a supported mode. A legacy "lock"
// mode becomes locked=true with append.
func (l *Layer) Normalize() {
	if l.Mode == "lock" {
		l.Locked = true
		l.Mode = ModeAppend
		return
	}
	switch l.Mode {
	case ModeInherit, ModeAppend, ModeReplace, ModeSuppress:
	default:
		l.Mode = ModeAppend
	}
}

// Result is a compiled prompt pair plus per-layer provenance.
type Result struct {
	Positive string
	Negative string
	Applied  []string
}

// Compile folds base (project-inherited context) and layers into a
// positive/negative prompt pair. Layers are applied in ascending priority
// order, so a higher priority layer is applied last and may override earlier
// text with replace. Locking never removes a layer's text.
func Compile(base string, baseNegative string, layers []Layer) Result {
	ordered := make([]Layer, len(layers))
	copy(ordered, layers)
	for i := range ordered {
		ordered[i].Normalize()
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })

	pos := strings.TrimSpace(base)
	negParts := []string{}
	if s := strings.TrimSpace(baseNegative); s != "" {
		negParts = append(negParts, s)
	}
	var applied []string
	for _, l := range ordered {
		posText := strings.TrimSpace(l.Positive)
		switch l.Mode {
		case ModeReplace:
			pos = posText
		case ModeSuppress:
			if posText != "" {
				negParts = append(negParts, posText)
			}
		default: // inherit, append
			if posText != "" {
				pos = joinNonEmpty(pos, posText)
			}
		}
		if neg := strings.TrimSpace(l.Negative); neg != "" {
			negParts = append(negParts, neg)
		}
		marker := l.Scope + ":" + l.Mode
		if l.Locked {
			marker += ":locked"
		}
		applied = append(applied, marker)
	}
	return Result{Positive: pos, Negative: strings.Join(negParts, ", "), Applied: applied}
}

func joinNonEmpty(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + ", " + b
}
