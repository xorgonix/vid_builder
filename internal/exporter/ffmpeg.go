// Package exporter assembles an edit into a single video with FFmpeg (spec §12.10, §15.7).
package exporter

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Clip is one edit item resolved to a local file.
type Clip struct {
	Path          string
	IsImage       bool
	DurationMs    int
	TransitionIn  string
	TransitionOut string
}

// Exporter runs FFmpeg.
type Exporter struct {
	Bin     string
	Width   int
	Height  int
	FPS     int
	Timeout time.Duration
}

// New constructs an exporter with sane defaults.
func New(bin string, width, height, fps int, timeoutSec int) *Exporter {
	if bin == "" {
		bin = "ffmpeg"
	}
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	if fps <= 0 {
		fps = 30
	}
	t := time.Duration(timeoutSec) * time.Second
	if t <= 0 {
		t = 30 * time.Minute
	}
	return &Exporter{Bin: bin, Width: width, Height: height, FPS: fps, Timeout: t}
}

// Available reports whether the FFmpeg binary can be executed.
func (e *Exporter) Available(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, e.Bin, "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg not available (%s): %w", e.Bin, err)
	}
	return nil
}

func transitionDurationMs(kind string) int {
	switch kind {
	case "dissolve":
		return 600
	case "fade":
		return 500
	default:
		return 40 // near-instant cut, expressed as a very short crossfade
	}
}

// Assemble builds a single output video from clips and writes it to outPath.
// Every clip is preserved. Transitions are composited with xfade so mixed
// cut/fade/dissolve sequences all retain their material.
func (e *Exporter) Assemble(ctx context.Context, clips []Clip, outPath string) error {
	if len(clips) == 0 {
		return fmt.Errorf("exporter: no clips")
	}
	args := []string{"-y"}
	for _, c := range clips {
		if c.IsImage {
			args = append(args, "-loop", "1", "-t", fmt.Sprintf("%.3f", float64(c.DurationMs)/1000.0), "-i", c.Path)
		} else {
			args = append(args, "-i", c.Path)
		}
	}

	var filters []string
	for i := range clips {
		filters = append(filters, fmt.Sprintf(
			"[%d:v]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p[v%d]",
			i, e.Width, e.Height, e.Width, e.Height, e.FPS, i))
	}

	if len(clips) == 1 {
		filters = append(filters, "[v0]null[outv]")
	} else {
		// Chain xfade transitions left to right.
		prev := "v0"
		var accumulatedMs int = clips[0].DurationMs
		for i := 1; i < len(clips); i++ {
			out := fmt.Sprintf("x%d", i)
			trans := clips[i-1].TransitionOut
			if trans == "" || trans == "cut" {
				trans = clips[i].TransitionIn
			}
			durMs := transitionDurationMs(trans)
			maxDur := clips[i-1].DurationMs
			if clips[i].DurationMs < maxDur {
				maxDur = clips[i].DurationMs
			}
			if durMs > maxDur/2 {
				durMs = maxDur / 2
			}
			if durMs < 1 {
				durMs = 1
			}
			offsetMs := accumulatedMs - durMs
			if offsetMs < 0 {
				offsetMs = 0
			}
			filters = append(filters, fmt.Sprintf(
				"[%s][v%d]xfade=transition=fade:duration=%.3f:offset=%.3f[%s]",
				prev, i, float64(durMs)/1000.0, float64(offsetMs)/1000.0, out))
			prev = out
			accumulatedMs = accumulatedMs + clips[i].DurationMs - durMs
		}
		filters = append(filters, fmt.Sprintf("[%s]null[outv]", prev))
	}

	args = append(args,
		"-filter_complex", strings.Join(filters, ";"),
		"-map", "[outv]",
		"-c:v", "libx264", "-preset", "medium", "-crf", "20",
		"-pix_fmt", "yuv420p", "-movflags", "+faststart",
		outPath)

	runCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, e.Bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, tail(string(out), 800))
	}
	return nil
}

// Probe returns a duration in ms for a media file using ffprobe-like parsing
// via ffmpeg is avoided; trim values are supplied by the edit instead.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

var _ = strconv.Itoa
