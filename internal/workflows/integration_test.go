package workflows_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/config"
	"vidbuild/internal/workflows"
)

// TestWanFirstLastFrameIntegration submits the bundled Wan first/last workflow to
// a live ComfyUI with the exact bindings the worker would apply, and verifies it
// produces a video output. It is skipped unless VIDBUILD_COMFYUI_URL is set so CI
// never depends on a GPU.
//
//	VIDBUILD_COMFYUI_URL=http://127.0.0.1:8188 go test ./internal/workflows -run TestWanFirstLastFrameIntegration -v -timeout 20m
func TestWanFirstLastFrameIntegration(t *testing.T) {
	base := os.Getenv("VIDBUILD_COMFYUI_URL")
	if base == "" {
		t.Skip("set VIDBUILD_COMFYUI_URL to run the live ComfyUI integration test")
	}

	all, err := workflows.LoadAll()
	if err != nil {
		t.Fatalf("load bundles: %v", err)
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
		t.Fatalf("clone graph: %v", err)
	}
	values := map[string]any{
		"first_frame_positive": "a potter shapes wet clay on a spinning wheel, warm studio light",
		"first_frame_negative": "lowres, blurry, distorted",
		"first_frame_asset":    "start.png",
		"last_frame_asset":     "end.png",
		"width":                512,
		"height":               320,
		"seed":                 int64(1234),
		"model_name":           "wan2.2_ti2v_5B_fp16.safetensors",
		"fps":                  8.0,
	}
	graph, err = workflows.ApplyBindings(graph, wan.Manifest.Bindings, values)
	if err != nil {
		t.Fatalf("apply bindings: %v", err)
	}

	cfg := config.Default().ComfyUI
	cfg.Enabled = true
	cfg.BaseURL = base
	cfg.RequestTimeoutSec = 60
	cfg.PollIntervalMs = 1500
	client := comfyui.New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()

	if _, err := client.Stats(ctx); err != nil {
		t.Fatalf("ComfyUI not reachable at %s: %v", base, err)
	}

	resp, err := client.Submit(ctx, graph, "vidbuild-integration")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	entry, err := client.Wait(ctx, resp.PromptID, 2*time.Second, nil)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if entry.Status.StatusStr != "success" {
		t.Fatalf("ComfyUI status = %q (messages: %+v)", entry.Status.StatusStr, entry.Status)
	}
	out, ok := entry.Outputs["12"]
	if !ok || len(out.Images) == 0 {
		t.Fatalf("no output on node 12: %+v", entry.Outputs)
	}
	name := out.Images[0].Filename
	if !strings.HasSuffix(strings.ToLower(name), ".webm") {
		t.Fatalf("expected a .webm video output, got %q", name)
	}
	body, err := client.Fetch(ctx, out.Images[0])
	if err != nil {
		t.Fatalf("fetch output: %v", err)
	}
	defer body.Close()
	buf := make([]byte, 4)
	if _, err := body.Read(buf); err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(buf) != "\x1a\x45\xdf\xa3" {
		t.Fatalf("output does not look like a Matroska/WebM stream: %q", buf)
	}
	t.Logf("verified Wan first/last output: %s", name)
}
