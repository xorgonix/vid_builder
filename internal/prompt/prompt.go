// Package prompt implements layered prompt compilation (spec §14.11A, §18.4).
// Layers are model-independent authority; compilation produces the exact
// positive/negative fields that will be bound into a workflow.
package prompt

import "strings"

// Layer modes.
const (
	ModeInherit  = "inherit"  // add inherited context
	ModeAppend   = "append"   // append to the compiled positive
	ModeReplace  = "replace"  // replace the compiled positive
	ModeSuppress = "suppress" // push text into the negative prompt
	ModeLock     = "lock"     // protect a layer from AI overwrite (no text effect)
)

// Layer is one editable prompt layer.
type Layer struct {
	Scope  string `json:"scope"`
	Mode   string `json:"mode"`
	Text   string `json:"text"`
	Locked bool   `json:"locked"`
}

// Result is a compiled prompt pair plus per-layer provenance.
type Result struct {
	Positive string
	Negative string
	Applied  []string
}

// Compile folds base (project-inherited context) and ordered layers into a
// positive/negative prompt pair.
func Compile(base string, baseNegative string, layers []Layer) Result {
	pos := strings.TrimSpace(base)
	negParts := []string{}
	if strings.TrimSpace(baseNegative) != "" {
		negParts = append(negParts, strings.TrimSpace(baseNegative))
	}
	var applied []string
	for _, l := range layers {
		text := strings.TrimSpace(l.Text)
		mode := l.Mode
		if mode == "" {
			mode = ModeAppend
		}
		switch mode {
		case ModeInherit, ModeAppend:
			if text != "" {
				pos = joinNonEmpty(pos, text)
			}
		case ModeReplace:
			pos = text
		case ModeSuppress:
			if text != "" {
				negParts = append(negParts, text)
			}
		case ModeLock:
			// No textual effect; locks protect the layer from AI edits.
		default:
			if text != "" {
				pos = joinNonEmpty(pos, text)
			}
		}
		applied = append(applied, l.Scope+":"+mode)
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
