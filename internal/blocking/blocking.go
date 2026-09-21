// Package blocking defines the structured blocking document (spec §8.4, §14.9)
// and renders it to deterministic SVG. All coordinates are normalized 0.0..1.0.
package blocking

import (
	"encoding/json"
	"strings"
)

// Spec is the blocking document for one endpoint of one shot.
type Spec struct {
	Version     int      `json:"version"`
	Canvas      Canvas   `json:"canvas"`
	Camera      Camera   `json:"camera"`
	Zones       []Zone   `json:"zones,omitempty"`
	Actors      []Actor  `json:"actors,omitempty"`
	Props       []Prop   `json:"props,omitempty"`
	Labels      []Label  `json:"labels,omitempty"`
	Arrows      []Arrow  `json:"arrows,omitempty"`
	Environment []string `json:"environment,omitempty"`
	Notes       string   `json:"notes,omitempty"`
}

// Canvas describes the frame aspect. Width/height are normalized units.
type Canvas struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Camera describes framing within the canvas.
type Camera struct {
	ShotSize      string  `json:"shot_size"` // wide, medium, close, extreme_close
	Angle         string  `json:"angle"`     // eye, low, high, dutch
	Height        float64 `json:"height"`    // normalized 0..1 eye height
	FocalCategory string  `json:"focal_category,omitempty"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	W             float64 `json:"w"`
	H             float64 `json:"h"`
}

// Zone is a foreground/midground/background region.
type Zone struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
}

// Actor is a subject with stable identity, pose and gaze.
type Actor struct {
	ElementID    string  `json:"element_id,omitempty"`
	CharacterID  string  `json:"character_id,omitempty"`
	Name         string  `json:"name"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	BodyRotation float64 `json:"body_rotation"` // degrees, 0 = facing camera
	PoseCategory string  `json:"pose_category,omitempty"`
	Pose         string  `json:"pose,omitempty"` // free text
	GazeX        float64 `json:"gaze_x"`
	GazeY        float64 `json:"gaze_y"`
	GazeTargetID string  `json:"gaze_target_id,omitempty"`
	Facing       string  `json:"facing,omitempty"` // left, right, camera, away
}

// Prop is an object in the frame.
type Prop struct {
	ElementID string  `json:"element_id,omitempty"`
	Name      string  `json:"name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	State     string  `json:"state,omitempty"`
}

// Label is a production note pinned to a point.
type Label struct {
	ElementID string  `json:"element_id,omitempty"`
	Text      string  `json:"text"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

// Arrow is a movement or camera arrow.
type Arrow struct {
	ElementID string  `json:"element_id,omitempty"`
	Kind      string  `json:"kind"` // movement, camera
	X1        float64 `json:"x1"`
	Y1        float64 `json:"y1"`
	X2        float64 `json:"x2"`
	Y2        float64 `json:"y2"`
}

// Default returns a valid, readable starting blocking.
func Default() Spec {
	return Spec{
		Version: SchemaVersion,
		Canvas:  Canvas{Width: 1, Height: 1},
		Camera:  Camera{ShotSize: "medium", Angle: "eye", Height: 0.5, X: 0.05, Y: 0.05, W: 0.9, H: 0.9},
		Zones: []Zone{
			{Name: "foreground", X: 0, Y: 0.6, W: 1, H: 0.4},
			{Name: "midground", X: 0, Y: 0.3, W: 1, H: 0.3},
			{Name: "background", X: 0, Y: 0, W: 1, H: 0.3},
		},
		Actors: []Actor{{ElementID: "actor_1", Name: "Subject", X: 0.5, Y: 0.55, BodyRotation: 0, PoseCategory: "standing", Pose: "standing", GazeX: 0.5, GazeY: 0.4, Facing: "camera"}},
	}
}

// NormalizeV2 upgrades a document in memory to schema version 2: it assigns
// stable element ids where missing and defaults pose categories. It never
// rewrites stored rows; a new version is persisted only on the next human save
// (spec §7.1, §2.5).
func (s *Spec) NormalizeV2() {
	if s.Version < 2 {
		s.Version = SchemaVersion
	}
	if s.Camera.Height == 0 {
		s.Camera.Height = 0.5
	}
	for i := range s.Actors {
		if s.Actors[i].ElementID == "" {
			s.Actors[i].ElementID = elementID("actor", i)
		}
		if s.Actors[i].PoseCategory == "" && s.Actors[i].Pose != "" {
			s.Actors[i].PoseCategory = poseCategory(s.Actors[i].Pose)
		}
	}
	for i := range s.Props {
		if s.Props[i].ElementID == "" {
			s.Props[i].ElementID = elementID("prop", i)
		}
	}
	for i := range s.Labels {
		if s.Labels[i].ElementID == "" {
			s.Labels[i].ElementID = elementID("label", i)
		}
	}
	for i := range s.Arrows {
		if s.Arrows[i].ElementID == "" {
			s.Arrows[i].ElementID = elementID("arrow", i)
		}
	}
}

func elementID(prefix string, i int) string {
	return prefix + "_" + itoa(i+1)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// poseCategory maps free text to the vocabulary when it clearly matches.
func poseCategory(pose string) string {
	switch strings.ToLower(strings.TrimSpace(pose)) {
	case "standing", "sitting", "walking", "running", "lying", "crouching", "kneeling", "reaching", "leaning":
		return strings.ToLower(strings.TrimSpace(pose))
	default:
		return "other"
	}
}

// Parse decodes blocking JSON, returning a default document on empty/invalid input.
func Parse(raw string) Spec {
	if raw == "" || raw == "{}" {
		return Default()
	}
	var s Spec
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Default()
	}
	if s.Version == 0 {
		s.Version = SchemaVersion
	}
	if s.Canvas.Width == 0 || s.Canvas.Height == 0 {
		s.Canvas = Canvas{Width: 1, Height: 1}
	}
	if s.Camera.W == 0 && s.Camera.H == 0 {
		s.Camera = Default().Camera
	}
	if s.Actors == nil {
		s.Actors = []Actor{}
	}
	s.NormalizeV2()
	return s
}

// Marshal renders the document as compact JSON.
func (s Spec) Marshal() string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Summary returns a short human description of the blocking.
func (s Spec) Summary() string {
	out := s.Camera.ShotSize + " / " + s.Camera.Angle
	if len(s.Actors) > 0 {
		out += " · " + s.Actors[0].Name
		if s.Actors[0].Pose != "" {
			out += " " + s.Actors[0].Pose
		}
	}
	return out
}

// Analyze reports the visible changes between two blockings (spec §14.9, §15.4).
func Analyze(start, end Spec) []string {
	var out []string
	if start.Camera.ShotSize != end.Camera.ShotSize {
		out = append(out, "Shot size changes: "+start.Camera.ShotSize+" → "+end.Camera.ShotSize)
	}
	if start.Camera.Angle != end.Camera.Angle {
		out = append(out, "Camera angle changes: "+start.Camera.Angle+" → "+end.Camera.Angle)
	}
	if abs(start.Camera.X-end.Camera.X) > 0.02 || abs(start.Camera.Y-end.Camera.Y) > 0.02 {
		out = append(out, "Camera position moves")
	}
	startByName := map[string]Actor{}
	for _, a := range start.Actors {
		startByName[actorKey(a)] = a
	}
	endByName := map[string]Actor{}
	for _, a := range end.Actors {
		endByName[actorKey(a)] = a
	}
	for key, a := range endByName {
		b, ok := startByName[key]
		if !ok {
			out = append(out, "Actor '"+a.Name+"' enters the frame")
			continue
		}
		if a.Pose != b.Pose {
			out = append(out, "Actor '"+a.Name+"' pose changes: "+b.Pose+" → "+a.Pose)
		}
		if dist(a.X, a.Y, b.X, b.Y) > 0.05 {
			out = append(out, "Actor '"+a.Name+"' changes position")
		}
		if dist(a.GazeX, a.GazeY, b.GazeX, b.GazeY) > 0.05 {
			out = append(out, "Actor '"+a.Name+"' gaze changes")
		}
		if a.Facing != b.Facing {
			out = append(out, "Actor '"+a.Name+"' turns: "+b.Facing+" → "+a.Facing)
		}
	}
	for key, a := range startByName {
		if _, ok := endByName[key]; !ok {
			out = append(out, "Actor '"+a.Name+"' leaves the frame")
		}
	}
	if len(start.Props) != len(end.Props) {
		out = append(out, "Props change")
	}
	if len(start.Arrows) != len(end.Arrows) {
		out = append(out, "Movement arrows change")
	}
	if len(out) == 0 {
		out = append(out, "No visible change detected between start and end — the shot lacks a payoff.")
	}
	return out
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func dist(x1, y1, x2, y2 float64) float64 {
	return (abs(x1-x2) + abs(y1-y2))
}

// actorKey identifies an actor for comparison and interpolation by stable id,
// falling back to the display name only for legacy documents (spec §7.1).
func actorKey(a Actor) string {
	if a.ElementID != "" {
		return a.ElementID
	}
	return a.Name
}

// Interpolate produces an in-between blocking at t (0 = start, 1 = end) by
// matching elements on stable IDs. It is used for animatic playback previews;
// it does not predict final model motion (spec §7.3, §11.1).
func Interpolate(start, end Spec, t float64) Spec {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	out := start
	out.NormalizeV2()
	end.NormalizeV2()

	lerp := func(a, b float64) float64 { return a + (b-a)*t }

	out.Camera.X = lerp(start.Camera.X, end.Camera.X)
	out.Camera.Y = lerp(start.Camera.Y, end.Camera.Y)
	out.Camera.W = lerp(start.Camera.W, end.Camera.W)
	out.Camera.H = lerp(start.Camera.H, end.Camera.H)
	out.Camera.Height = lerp(start.Camera.Height, end.Camera.Height)
	if t >= 0.5 {
		out.Camera.ShotSize = end.Camera.ShotSize
		out.Camera.Angle = end.Camera.Angle
	}

	endActors := map[string]Actor{}
	for _, a := range end.Actors {
		endActors[actorKey(a)] = a
	}
	out.Actors = make([]Actor, 0, len(start.Actors))
	for _, a := range start.Actors {
		if b, ok := endActors[actorKey(a)]; ok {
			a.X = lerp(a.X, b.X)
			a.Y = lerp(a.Y, b.Y)
			a.GazeX = lerp(a.GazeX, b.GazeX)
			a.GazeY = lerp(a.GazeY, b.GazeY)
			a.BodyRotation = lerp(a.BodyRotation, b.BodyRotation)
		}
		out.Actors = append(out.Actors, a)
	}

	endProps := map[string]Prop{}
	for _, p := range end.Props {
		endProps[p.ElementID] = p
	}
	out.Props = make([]Prop, 0, len(start.Props))
	for _, p := range start.Props {
		if q, ok := endProps[p.ElementID]; ok {
			p.X = lerp(p.X, q.X)
			p.Y = lerp(p.Y, q.Y)
		}
		out.Props = append(out.Props, p)
	}

	endArrows := map[string]Arrow{}
	for _, ar := range end.Arrows {
		endArrows[ar.ElementID] = ar
	}
	out.Arrows = make([]Arrow, 0, len(start.Arrows))
	for _, ar := range start.Arrows {
		if q, ok := endArrows[ar.ElementID]; ok {
			ar.X1 = lerp(ar.X1, q.X1)
			ar.Y1 = lerp(ar.Y1, q.Y1)
			ar.X2 = lerp(ar.X2, q.X2)
			ar.Y2 = lerp(ar.Y2, q.Y2)
		}
		out.Arrows = append(out.Arrows, ar)
	}
	return out
}
