package blocking

import (
	"fmt"
	"math"
	"strings"
)

const svgWidth = 1000.0

// RenderSVG renders a blocking document as deterministic SVG markup. The same
// input always produces byte-identical output (no randomness, no time).
func RenderSVG(s Spec) string {
	h := svgWidth
	if s.Canvas.Width > 0 {
		h = svgWidth * s.Canvas.Height / s.Canvas.Width
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="100%%" role="img" aria-label="Blocking diagram">`, int(svgWidth), int(h))
	fmt.Fprintf(&b, `<defs><marker id="ah" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 z" fill="#d97706"/></marker></defs>`)
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%d" height="%d" fill="#0b0f14" stroke="#334155" stroke-width="2"/>`, int(svgWidth), int(h))

	// Zones (background first).
	for _, z := range s.Zones {
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#1e293b" fill-opacity="0.45" stroke="#334155" stroke-dasharray="4 4"/>`,
			z.X*svgWidth, z.Y*h, z.W*svgWidth, z.H*h)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#64748b" font-size="16" font-family="sans-serif">%s</text>`,
			z.X*svgWidth+8, z.Y*h+22, xmlEscape(z.Name))
	}

	// Camera frame.
	fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="none" stroke="#38bdf8" stroke-width="3" stroke-dasharray="10 6"/>`,
		s.Camera.X*svgWidth, s.Camera.Y*h, s.Camera.W*svgWidth, s.Camera.H*h)

	// Arrows.
	for _, a := range s.Arrows {
		color := "#d97706"
		if a.Kind == "camera" {
			color = "#38bdf8"
		}
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="3" marker-end="url(#ah)"/>`,
			a.X1*svgWidth, a.Y1*h, a.X2*svgWidth, a.Y2*h, color)
	}

	// Props.
	for _, p := range s.Props {
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="36" height="36" fill="#a78bfa" fill-opacity="0.7" rx="4"/>`,
			p.X*svgWidth-18, p.Y*h-18)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#c4b5fd" font-size="14" font-family="sans-serif" text-anchor="middle">%s</text>`,
			p.X*svgWidth, p.Y*h+48, xmlEscape(p.Name))
	}

	// Actors.
	for _, a := range s.Actors {
		cx, cy := a.X*svgWidth, a.Y*h
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="26" fill="#f472b6" fill-opacity="0.85" stroke="#be185d" stroke-width="2"/>`, cx, cy)
		// Head direction from body rotation.
		rad := (a.BodyRotation - 90) * math.Pi / 180
		hx, hy := cx+math.Cos(rad)*30, cy+math.Sin(rad)*30
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#fbcfe8" stroke-width="4"/>`, cx, cy, hx, hy)
		// Gaze arrow.
		gx, gy := a.GazeX*svgWidth, a.GazeY*h
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#fde68a" stroke-width="3" stroke-dasharray="6 4" marker-end="url(#ah)"/>`, cx, cy, gx, gy)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#fbcfe8" font-size="16" font-family="sans-serif" text-anchor="middle">%s</text>`, cx, cy+52, xmlEscape(a.Name))
	}

	// Labels.
	for _, l := range s.Labels {
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#e2e8f0" font-size="16" font-family="sans-serif">%s</text>`,
			l.X*svgWidth, l.Y*h, xmlEscape(l.Text))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}
