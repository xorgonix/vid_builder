package comfyui_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/config"
	"vidbuild/internal/testutil"
)

func clientFor(fake *testutil.ComfyUI) *comfyui.Client {
	cfg := config.Default().ComfyUI
	cfg.BaseURL = fake.URL
	cfg.RequestTimeoutSec = 10
	cfg.PollIntervalMs = 10
	return comfyui.New(cfg)
}

func TestStatsAndCheckpoints(t *testing.T) {
	fake := testutil.NewComfyUI(t)
	c := clientFor(fake)
	ctx := context.Background()

	stats, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.System.ComfyUIVersion == "" || len(stats.Devices) == 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	cps, err := c.Checkpoints(ctx)
	if err != nil {
		t.Fatalf("checkpoints: %v", err)
	}
	if len(cps) != 1 || cps[0] != "photon_v1.safetensors" {
		t.Fatalf("checkpoints = %v", cps)
	}
}

func TestHasNode(t *testing.T) {
	fake := testutil.NewComfyUI(t)
	c := clientFor(fake)
	ok, err := c.HasNode(context.Background(), "KSampler")
	if err != nil || !ok {
		t.Fatalf("HasNode = %v, %v", ok, err)
	}
}

func TestSubmitWaitFetch(t *testing.T) {
	fake := testutil.NewComfyUI(t)
	c := clientFor(fake)
	ctx := context.Background()

	resp, err := c.Submit(ctx, map[string]any{"1": map[string]any{"class_type": "KSampler", "inputs": map[string]any{}}}, "test-client")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if resp.PromptID == "" {
		t.Fatal("empty prompt id")
	}
	entry, err := c.Wait(ctx, resp.PromptID, 0, nil)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if entry.Status.StatusStr != "success" {
		t.Fatalf("status = %q", entry.Status.StatusStr)
	}
	out, ok := entry.Outputs["7"]
	if !ok || len(out.Images) == 0 {
		t.Fatalf("no images in outputs: %+v", entry.Outputs)
	}
	body, err := c.Fetch(ctx, out.Images[0])
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer body.Close()
	b, _ := io.ReadAll(body)
	if len(b) == 0 {
		t.Fatal("empty fetched image")
	}
}

func TestUploadImage(t *testing.T) {
	fake := testutil.NewComfyUI(t)
	c := clientFor(fake)

	up, err := c.UploadImage(context.Background(), "frame.png", strings.NewReader("not-really-a-png"), "")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if up.Name == "" {
		t.Fatal("empty upload name")
	}
	if fake.Uploads() != 1 {
		t.Fatalf("uploads = %d", fake.Uploads())
	}
}

func TestUnreachableServerErrors(t *testing.T) {
	fake := testutil.NewComfyUI(t)
	fake.SetDown(true)
	c := clientFor(fake)
	if _, err := c.Stats(context.Background()); err == nil {
		t.Fatal("expected error when ComfyUI is down")
	}
}
