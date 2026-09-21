package worker

import (
	"testing"

	"vidbuild/internal/workflows"
)

func TestOutputMediaClassification(t *testing.T) {
	cases := map[string][2]string{
		"vidbuild_00001_.png": {"image", "image/png"},
		"clip_00001_.webm":    {"video", "video/webm"},
		"clip_00001_.mp4":     {"video", "video/mp4"},
		"frame_00001_.webp":   {"image", "image/webp"},
		"frame_00001_.jpg":    {"image", "image/jpeg"},
		"frame_00001_.JPEG":   {"image", "image/jpeg"},
		"anim_00001_.gif":     {"image", "image/gif"},
		"unknown_00001_":      {"image", "image/png"},
	}
	for name, want := range cases {
		kind, mime := outputMedia(name)
		if kind != want[0] || mime != want[1] {
			t.Fatalf("outputMedia(%q) = (%q,%q), want (%q,%q)", name, kind, mime, want[0], want[1])
		}
	}
}

func TestBindingValuesSuppliesWanControls(t *testing.T) {
	payload := requestPayload{
		Positive: "start", Negative: "no", Width: 512, Height: 320,
		Checkpoint: "wan2.2_ti2v_5B_fp16.safetensors",
		FrameCount: 49, FPS: 12,
		MotionPositive: "she turns", MotionNegative: "no shake",
		LastPositive: "end", LastNegative: "no blur",
	}
	values := bindingValues(payload, 99)
	for field, want := range map[string]any{
		"first_frame_positive": "start",
		"first_frame_negative": "no",
		"width":                512,
		"height":               320,
		"seed":                 int64(99),
		"model_name":           "wan2.2_ti2v_5B_fp16.safetensors",
		"frame_count":          49,
		"fps":                  12.0,
		"motion_positive":      "she turns",
		"motion_negative":      "no shake",
		"last_frame_positive":  "end",
		"last_frame_negative":  "no blur",
	} {
		if values[field] != want {
			t.Fatalf("value %q = %v, want %v", field, values[field], want)
		}
	}
}

func TestBindingValuesOmitsUnsetVideoControls(t *testing.T) {
	values := bindingValues(requestPayload{Positive: "p", Negative: "n", Width: 1, Height: 1}, 1)
	for _, field := range []string{"frame_count", "fps", "motion_positive", "last_frame_positive"} {
		if _, ok := values[field]; ok {
			t.Fatalf("%q should be omitted when unset so workflow defaults stay authoritative", field)
		}
	}
}

// TestWanPackageBindsWorkerValues proves the worker's semantic values resolve
// against the bundled Wan package exactly as ApplyBindings would at runtime.
func TestWanPackageBindsWorkerValues(t *testing.T) {
	all, err := workflows.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	var wan *workflows.Packaged
	for _, p := range all {
		if p.Manifest.Slug == "wan-flf-video" {
			wan = p
		}
	}
	if wan == nil {
		t.Fatal("wan-flf-video package not found")
	}
	graph, err := workflows.CloneGraph(wan.GraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	values := bindingValues(requestPayload{
		Positive: "a potter at the wheel", Negative: "lowres",
		Width: 512, Height: 320, FrameCount: 25, FPS: 8,
		Checkpoint: "wan2.2_ti2v_5B_fp16.safetensors",
	}, 7)
	values["first_frame_asset"] = "start.png"
	values["last_frame_asset"] = "end.png"
	if _, err := workflows.ApplyBindings(graph, wan.Manifest.Bindings, values); err != nil {
		t.Fatalf("worker values did not resolve against the Wan package: %v", err)
	}
}
