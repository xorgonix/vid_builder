package prompt

import (
	"strings"
)

// LintWarning is one advisory prompt-linting result (spec §9.5).
type LintWarning struct {
	Code    string
	Message string
}

// LintInput is the material linting needs. Callers compute the blocking-derived
// flags so this package stays free of visual dependencies.
type LintInput struct {
	StartPositive     string
	EndPositive       string
	MotionPositive    string
	NoVisibleChange   bool
	InvariantChanged  bool
	PromptTokens      int
	BudgetTokens      int
	DroppedStructured []string
}

// Lint returns advisory warnings. Only invalid workflow bindings or missing
// required fields are fatal elsewhere; these are guidance (spec §9.5).
func Lint(in LintInput) []LintWarning {
	var out []LintWarning
	add := func(code, msg string) { out = append(out, LintWarning{Code: code, Message: msg}) }

	start := normalize(in.StartPositive)
	end := normalize(in.EndPositive)
	if start != "" && end != "" {
		if start == end {
			add("identical_endpoints", "Start and end prompts are identical; the shot has no visible change.")
		} else if similar(start, end) {
			add("near_identical_endpoints", "Start and end prompts are nearly identical; make the change explicit.")
		}
	}
	if in.NoVisibleChange {
		add("no_visible_change", "No visible state change detected between start and end.")
	}
	if hasContradiction(in.MotionPositive) {
		add("contradictory_camera", "Motion text contains contradictory camera commands.")
	}
	if conflictingLighting(in.StartPositive + " " + in.EndPositive) {
		add("conflicting_lighting", "Lighting or colour instructions appear to conflict.")
	}
	if countPrimaryActions(in.MotionPositive) > 2 {
		add("too_many_actions", "More than two primary actions in one shot; consider splitting it.")
	}
	if in.InvariantChanged {
		add("invariant_changed", "A project-bible invariant changed without explicit authorization.")
	}
	if in.BudgetTokens > 0 && in.PromptTokens > in.BudgetTokens {
		add("over_budget", "Prompt exceeds the selected model/workflow token budget.")
	}
	if len(in.DroppedStructured) > 0 {
		add("dropped_fact", "A structured fact was dropped during lossy compilation: "+strings.Join(in.DroppedStructured, ", ")+".")
	}
	return out
}

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// similar reports whether two prompts are near-identical by token overlap.
func similar(a, b string) bool {
	at := strings.Fields(a)
	bt := strings.Fields(b)
	if len(at) == 0 || len(bt) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, t := range at {
		set[t] = true
	}
	common := 0
	for _, t := range bt {
		if set[t] {
			common++
		}
	}
	denom := len(at)
	if len(bt) > denom {
		denom = len(bt)
	}
	return float64(common)/float64(denom) >= 0.85
}

var cameraPushPull = [][2]string{
	{"push in", "pull out"},
	{"pan left", "pan right"},
	{"tilt up", "tilt down"},
	{"dolly in", "dolly out"},
}

func hasContradiction(s string) bool {
	l := strings.ToLower(s)
	for _, pair := range cameraPushPull {
		if strings.Contains(l, pair[0]) && strings.Contains(l, pair[1]) {
			return true
		}
	}
	if strings.Contains(l, "static") && (strings.Contains(l, "push in") || strings.Contains(l, "pan ") || strings.Contains(l, "orbit")) {
		return true
	}
	return false
}

var lightingPairs = [][2]string{
	{"bright", "dark"},
	{"warm", "cool"},
	{"daylight", "night"},
	{"soft light", "hard light"},
}

func conflictingLighting(s string) bool {
	l := strings.ToLower(s)
	for _, pair := range lightingPairs {
		if strings.Contains(l, pair[0]) && strings.Contains(l, pair[1]) {
			return true
		}
	}
	return false
}

// countPrimaryActions counts sentence and semicolon separated actions.
func countPrimaryActions(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	replaced := strings.NewReplacer(";", ".", "\n", ".").Replace(s)
	count := 0
	for _, part := range strings.Split(replaced, ".") {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}
