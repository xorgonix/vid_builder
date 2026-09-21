package blocking

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// SchemaVersion is the current blocking document version (spec §7, §13.3).
const SchemaVersion = 2

var (
	shotSizes = map[string]bool{"wide": true, "medium": true, "close": true, "extreme_close": true}
	angles    = map[string]bool{"eye": true, "low": true, "high": true, "dutch": true}
	facings   = map[string]bool{"left": true, "right": true, "camera": true, "away": true}
	focalCats = map[string]bool{"wide": true, "normal": true, "telephoto": true, "macro": true, "fisheye": true}
	poseCats  = map[string]bool{
		"standing": true, "sitting": true, "walking": true, "running": true, "lying": true,
		"crouching": true, "kneeling": true, "reaching": true, "leaning": true, "other": true,
	}
)

// Validate checks the document against the versioned schema. Coordinates are
// normalized 0..1 and must be finite: NaN, infinity and out-of-range values are
// rejected (spec §13.3).
func (s Spec) Validate() error {
	var issues []string
	add := func(format string, args ...any) { issues = append(issues, fmt.Sprintf(format, args...)) }

	if s.Version < 1 || s.Version > SchemaVersion {
		add("unsupported schema version %d (want 1..%d)", s.Version, SchemaVersion)
	}

	if !finite(s.Canvas.Width) || !finite(s.Canvas.Height) {
		add("canvas width/height must be finite numbers")
	} else if s.Canvas.Width <= 0 || s.Canvas.Height <= 0 || s.Canvas.Width > 8 || s.Canvas.Height > 8 {
		add("canvas width/height out of range (0, 8]")
	}

	checkUnitRect(add, "camera", s.Camera.X, s.Camera.Y, s.Camera.W, s.Camera.H)
	if !finite(s.Camera.Height) || s.Camera.Height < 0 || s.Camera.Height > 1 {
		add("camera height must be between 0 and 1")
	}
	if s.Camera.ShotSize != "" && !shotSizes[s.Camera.ShotSize] {
		add("camera shot_size %q is not one of wide, medium, close, extreme_close", s.Camera.ShotSize)
	}
	if s.Camera.Angle != "" && !angles[s.Camera.Angle] {
		add("camera angle %q is not one of eye, low, high, dutch", s.Camera.Angle)
	}
	if s.Camera.FocalCategory != "" && !focalCats[s.Camera.FocalCategory] {
		add("camera focal_category %q is not one of wide, normal, telephoto, macro, fisheye", s.Camera.FocalCategory)
	}

	for i, z := range s.Zones {
		checkUnitRect(add, fmt.Sprintf("zone[%d]", i), z.X, z.Y, z.W, z.H)
	}
	actorIDs := map[string]bool{}
	for i, a := range s.Actors {
		if strings.TrimSpace(a.Name) == "" {
			add("actor[%d] name is required", i)
		}
		if a.ElementID == "" {
			add("actor[%d] is missing a stable element_id", i)
		} else if actorIDs[a.ElementID] {
			add("actor element_id %q is duplicated", a.ElementID)
		} else {
			actorIDs[a.ElementID] = true
		}
		if a.PoseCategory != "" && !poseCats[a.PoseCategory] {
			add("actor[%d] pose_category %q is not a known pose", i, a.PoseCategory)
		}
		checkUnitPoint(add, fmt.Sprintf("actor[%d] position", i), a.X, a.Y)
		checkUnitPoint(add, fmt.Sprintf("actor[%d] gaze", i), a.GazeX, a.GazeY)
		if !finite(a.BodyRotation) || a.BodyRotation < -360 || a.BodyRotation > 360 {
			add("actor[%d] body_rotation must be between -360 and 360", i)
		}
		if a.Facing != "" && !facings[a.Facing] {
			add("actor[%d] facing %q is not one of left, right, camera, away", i, a.Facing)
		}
	}
	propIDs := map[string]bool{}
	for i, p := range s.Props {
		if strings.TrimSpace(p.Name) == "" {
			add("prop[%d] name is required", i)
		}
		if p.ElementID == "" {
			add("prop[%d] is missing a stable element_id", i)
		} else if propIDs[p.ElementID] {
			add("prop element_id %q is duplicated", p.ElementID)
		} else {
			propIDs[p.ElementID] = true
		}
		checkUnitPoint(add, fmt.Sprintf("prop[%d]", i), p.X, p.Y)
	}
	for i, l := range s.Labels {
		checkUnitPoint(add, fmt.Sprintf("label[%d]", i), l.X, l.Y)
	}
	for i, ar := range s.Arrows {
		if ar.Kind != "" && ar.Kind != "movement" && ar.Kind != "camera" {
			add("arrow[%d] kind %q is not one of movement, camera", i, ar.Kind)
		}
		checkUnitPoint(add, fmt.Sprintf("arrow[%d] start", i), ar.X1, ar.Y1)
		checkUnitPoint(add, fmt.Sprintf("arrow[%d] end", i), ar.X2, ar.Y2)
	}

	if len(issues) > 0 {
		return fmt.Errorf("blocking: %s", strings.Join(issues, "; "))
	}
	return nil
}

// ValidateJSON decodes raw JSON, upgrades it in memory to v2 and validates it.
func ValidateJSON(raw string) error {
	var s Spec
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return fmt.Errorf("blocking: invalid JSON: %w", err)
	}
	s.NormalizeV2()
	return s.Validate()
}

func checkUnitPoint(add func(string, ...any), label string, x, y float64) {
	if !finite(x) || !finite(y) {
		add("%s must be finite numbers", label)
		return
	}
	if !unit(x) || !unit(y) {
		add("%s out of range 0..1", label)
	}
}

func checkUnitRect(add func(string, ...any), label string, x, y, w, h float64) {
	if !finite(x) || !finite(y) || !finite(w) || !finite(h) {
		add("%s must be finite numbers", label)
		return
	}
	if !unit(x) || !unit(y) {
		add("%s position out of range 0..1", label)
	}
	if w <= 0 || h <= 0 || !unit(w) || !unit(h) {
		add("%s size out of range (0, 1]", label)
	}
}

func unit(v float64) bool { return v >= 0 && v <= 1 }

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
