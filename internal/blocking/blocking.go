// Package blocking defines the structured blocking document (spec §8.4, §14.9)
// and renders it to deterministic SVG. All coordinates are normalized 0.0..1.0.
package blocking

import "encoding/json"

// Spec is the blocking document for one endpoint of one shot.
type Spec struct {
	Canvas Canvas  `json:"canvas"`
	Camera Camera  `json:"camera"`
	Zones  []Zone  `json:"zones,omitempty"`
	Actors []Actor `json:"actors,omitempty"`
	Props  []Prop  `json:"props,omitempty"`
	Labels []Label `json:"labels,omitempty"`
	Arrows []Arrow `json:"arrows,omitempty"`
	Notes  string  `json:"notes,omitempty"`
}

// Canvas describes the frame aspect. Width/height are normalized units.
type Canvas struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Camera describes framing within the canvas.
type Camera struct {
	ShotSize string  `json:"shot_size"` // wide, medium, close, extreme_close
	Angle    string  `json:"angle"`     // eye, low, high, dutch
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
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
	CharacterID  string  `json:"character_id,omitempty"`
	Name         string  `json:"name"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	BodyRotation float64 `json:"body_rotation"` // degrees, 0 = facing camera
	Pose         string  `json:"pose,omitempty"`
	GazeX        float64 `json:"gaze_x"`
	GazeY        float64 `json:"gaze_y"`
	Facing       string  `json:"facing,omitempty"` // left, right, camera, away
}

// Prop is an object in the frame.
type Prop struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// Label is a production note pinned to a point.
type Label struct {
	Text string  `json:"text"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// Arrow is a movement or camera arrow.
type Arrow struct {
	Kind string  `json:"kind"` // movement, camera
	X1   float64 `json:"x1"`
	Y1   float64 `json:"y1"`
	X2   float64 `json:"x2"`
	Y2   float64 `json:"y2"`
}

// Default returns a valid, readable starting blocking.
func Default() Spec {
	return Spec{
		Canvas: Canvas{Width: 1, Height: 1},
		Camera: Camera{ShotSize: "medium", Angle: "eye", X: 0.05, Y: 0.05, W: 0.9, H: 0.9},
		Zones: []Zone{
			{Name: "foreground", X: 0, Y: 0.6, W: 1, H: 0.4},
			{Name: "midground", X: 0, Y: 0.3, W: 1, H: 0.3},
			{Name: "background", X: 0, Y: 0, W: 1, H: 0.3},
		},
		Actors: []Actor{{Name: "Subject", X: 0.5, Y: 0.55, BodyRotation: 0, Pose: "standing", GazeX: 0.5, GazeY: 0.4, Facing: "camera"}},
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
	if s.Canvas.Width == 0 || s.Canvas.Height == 0 {
		s.Canvas = Canvas{Width: 1, Height: 1}
	}
	if s.Camera.W == 0 && s.Camera.H == 0 {
		s.Camera = Default().Camera
	}
	if s.Actors == nil {
		s.Actors = []Actor{}
	}
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
		startByName[a.Name] = a
	}
	endByName := map[string]Actor{}
	for _, a := range end.Actors {
		endByName[a.Name] = a
	}
	for name, a := range endByName {
		b, ok := startByName[name]
		if !ok {
			out = append(out, "Actor '"+name+"' enters the frame")
			continue
		}
		if a.Pose != b.Pose {
			out = append(out, "Actor '"+name+"' pose changes: "+b.Pose+" → "+a.Pose)
		}
		if dist(a.X, a.Y, b.X, b.Y) > 0.05 {
			out = append(out, "Actor '"+name+"' changes position")
		}
		if dist(a.GazeX, a.GazeY, b.GazeX, b.GazeY) > 0.05 {
			out = append(out, "Actor '"+name+"' gaze changes")
		}
		if a.Facing != b.Facing {
			out = append(out, "Actor '"+name+"' turns: "+b.Facing+" → "+a.Facing)
		}
	}
	for name := range startByName {
		if _, ok := endByName[name]; !ok {
			out = append(out, "Actor '"+name+"' leaves the frame")
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
