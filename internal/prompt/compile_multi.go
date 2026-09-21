package prompt

import (
	"fmt"
	"strings"
)

// Products are the compiled prompt fields a workflow may bind (spec §9.3).
// A workflow with a single prompt input receives an explicit lossy merge report
// instead of pretending these stay independent.
type Products struct {
	FirstPositive     string
	FirstNegative     string
	LastPositive      string
	LastNegative      string
	MotionPositive    string
	MotionNegative    string
	ReferenceGuidance string
	Auxiliary         map[string]string
}

// endpointScopes route a layer to a specific compiled product.
var endpointScopes = map[string]bool{"first_frame": true, "last_frame": true, "motion": true}

// CompileProducts compiles one shot's layers into separate endpoint and motion
// products. Layers whose scope is first_frame/last_frame/motion apply only to
// that product; every other layer (project style, character, location,
// workflow, experiment) is shared.
func CompileProducts(base, baseNegative string, layers []Layer) Products {
	shared := make([]Layer, 0, len(layers))
	byScope := map[string][]Layer{}
	for _, l := range layers {
		if endpointScopes[l.Scope] {
			byScope[l.Scope] = append(byScope[l.Scope], l)
			continue
		}
		shared = append(shared, l)
	}
	with := func(scope string) []Layer {
		out := make([]Layer, 0, len(shared)+len(byScope[scope]))
		out = append(out, shared...)
		out = append(out, byScope[scope]...)
		return out
	}

	first := Compile(base, baseNegative, with("first_frame"))
	last := Compile(base, baseNegative, with("last_frame"))
	motion := Compile("", "", with("motion"))

	p := Products{
		FirstPositive:  first.Positive,
		FirstNegative:  first.Negative,
		LastPositive:   last.Positive,
		LastNegative:   last.Negative,
		MotionPositive: motion.Positive,
		MotionNegative: motion.Negative,
		Auxiliary:      map[string]string{},
	}

	// Reference/identity guidance is the compiled character-scope material.
	var identity []string
	for _, l := range shared {
		if l.Scope == "character" && strings.TrimSpace(l.Positive) != "" {
			identity = append(identity, strings.TrimSpace(l.Positive))
		}
		if l.Scope == "workflow" && strings.TrimSpace(l.Positive) != "" {
			p.Auxiliary[l.Scope] = strings.TrimSpace(l.Positive)
		}
	}
	p.ReferenceGuidance = strings.Join(identity, ", ")
	return p
}

// MergeReport explains how separate products collapse for a single-prompt
// workflow (spec §9.3). It never silently pretends fields remain independent.
type MergeReport struct {
	Lossy        bool
	MergedFields []string
	DroppedFacts []string
	Note         string
}

// MergeProducts folds the products into one positive/negative pair for a
// single-prompt workflow and reports exactly what was merged or dropped.
func MergeProducts(p Products) (positive, negative string, report MergeReport) {
	var posParts, negParts, merged []string
	appendUnique := func(list []string, v string) []string {
		v = strings.TrimSpace(v)
		if v == "" {
			return list
		}
		for _, existing := range list {
			if existing == v {
				return list
			}
		}
		return append(list, v)
	}

	posParts = appendUnique(posParts, p.FirstPositive)
	if p.LastPositive != "" && p.LastPositive != p.FirstPositive {
		posParts = appendUnique(posParts, p.LastPositive)
		merged = append(merged, "last frame folded into the single positive prompt")
	}
	if p.MotionPositive != "" {
		posParts = appendUnique(posParts, p.MotionPositive)
		merged = append(merged, "motion folded into the single positive prompt")
	}
	negParts = appendUnique(negParts, p.FirstNegative)
	negParts = appendUnique(negParts, p.LastNegative)
	negParts = appendUnique(negParts, p.MotionNegative)

	if p.ReferenceGuidance != "" {
		merged = append(merged, "reference/identity guidance folded into the positive prompt")
		posParts = appendUnique(posParts, p.ReferenceGuidance)
	}

	report.MergedFields = merged
	report.Lossy = len(merged) > 0
	if report.Lossy {
		report.Note = "This workflow exposes one prompt input, so separate fields were merged; independence is not preserved."
	}
	return strings.Join(posParts, ", "), strings.Join(negParts, ", "), report
}

// WanFields are the ordered elements of a Wan-oriented shot prompt (spec §9.4).
type WanFields struct {
	Subject         string
	Setting         string
	InitialState    string
	Action          string
	EndState        string
	SecondaryMotion string
	ShotCamera      string
	LightingColour  string
	StyleMood       string
	Continuity      string
}

// WanOrder renders the fields in the required order. The ending state is used
// as given; it must be derived from the accepted end state, not a rewrite of
// the start (spec §9.4).
func WanOrder(f WanFields) string {
	ordered := []string{
		joinPair(f.Subject, f.Setting),
		f.InitialState,
		f.Action,
		f.EndState,
		f.SecondaryMotion,
		f.ShotCamera,
		f.LightingColour,
		f.StyleMood,
		f.Continuity,
	}
	parts := make([]string, 0, len(ordered))
	for _, s := range ordered {
		if t := strings.TrimSpace(s); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " → ")
}

func joinPair(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + ", " + b
	}
}

// EndPromptFromFacts builds the end prompt from an accepted end state's facts,
// so the ending is derived rather than a light rewrite of the start (spec §9.4).
func EndPromptFromFacts(facts map[string]string) string {
	order := []string{
		"subject", "setting", "state", "action", "lighting", "colour", "props",
		"environment", "weather", "time", "emotion", "continuity",
	}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		if v := strings.TrimSpace(facts[k]); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}

// FormatMergeReport renders a short human note for the UI.
func FormatMergeReport(r MergeReport) string {
	if !r.Lossy {
		return ""
	}
	return fmt.Sprintf("%s (%d field group(s) merged)", r.Note, len(r.MergedFields))
}
