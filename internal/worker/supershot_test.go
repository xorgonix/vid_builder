package worker

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/media"
	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
)

const refImageManifest = `{
  "name": "Ref Image", "slug": "ref-image", "task": "image",
  "capabilities": {
    "inputs": { "start_image": true, "end_image": true, "reference_image": true },
    "continuation": { "mode": "anchor_only" }, "verified": false
  }
}`

const refImageGraph = `{
  "4": {"class_type": "CLIPTextEncode", "inputs": {"text": ""}},
  "7": {"class_type": "SaveImage", "inputs": {}},
  "10": {"class_type": "LoadImage", "inputs": {"image": "placeholder.png"}}
}`

// TestGenerateAnchorStagesReferences proves C03/T04: when the chosen image
// workflow declares reference bindings, the supplied endpoints and the previous
// anchor reach the submitted graph, and the anchor records its own attempt.
func TestGenerateAnchorStagesReferences(t *testing.T) {
	st := testutil.NewStore(t)
	fake := testutil.NewComfyUI(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Refs")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")

	mediaStore := media.New(t.TempDir())
	mkAsset := func(role, name string, content string) string {
		uri, checksum, size, err := mediaStore.Put("endpoints", name, strings.NewReader(content))
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		id, err := st.CreateAsset(t.Context(), store.Asset{WorkspaceID: ws.ID, ProjectID: project.ID, Kind: "image", Role: role, StorageURI: uri, MimeType: "image/png", SizeBytes: size, Checksum: checksum})
		if err != nil {
			t.Fatalf("asset %s: %v", name, err)
		}
		return id
	}
	startA := mkAsset("start_frame", "start.png", "start-bytes")
	endA := mkAsset("end_frame", "end.png", "end-bytes")

	bindings := []store.WorkflowBinding{
		{SemanticField: "first_frame_asset", NodeID: "10", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "reference_asset", NodeID: "10", InputName: "image", ValueType: "string", Required: true},
		{SemanticField: "first_frame_positive", NodeID: "4", InputName: "text", ValueType: "string", Required: true},
		{SemanticField: "output_node", NodeID: "7", InputName: "", ValueType: "node", Required: true},
	}
	if err := st.SeedBundledWorkflow(t.Context(), "Ref Image", "ref-image", "image", "test", refImageGraph, "refsha", refImageManifest, "0.35.0", bindings); err != nil {
		t.Fatalf("seed image workflow: %v", err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	imageVersion := ""
	for _, p := range pkgs {
		if p.Slug == "ref-image" && p.LatestVersion.Valid {
			imageVersion = p.LatestVersion.String
		}
	}
	if imageVersion == "" {
		t.Fatal("no image workflow version")
	}

	spec, err := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}
	plan, err := st.CreateSuperShotPlan(t.Context(), store.SuperShotRequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version, WorkflowVersionID: imageVersion,
		ImageWorkflowVersionID: imageVersion,
		Strategy: "chained", RequestedDurationMS: 15000, PlannedDurationMS: 10000,
		FPSNum: 8, FPSDen: 1, Width: 512, Height: 320,
		BasePositive: "walk", CompleteMovement: "walk to the chair", ShotPurpose: "show calm",
		PlannerStatus: "manual",
	}, []store.AnchorInput{
		{Index: 0, Kind: "supplied_start", StatePrompt: "standing", AssetID: startA},
		{Index: 1, Kind: "intermediate", StatePrompt: "beside the chair", TimeOffsetMS: 5000},
		{Index: 2, Kind: "supplied_end", StatePrompt: "seated", TimeOffsetMS: 10000, AssetID: endA},
	}, []store.SegmentInput{
		{RawAction: "approach", ActionPrompt: "approach", DurationMS: 5000, StartOffsetMS: 0, EndOffsetMS: 5000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
		{RawAction: "sit", ActionPrompt: "sit", DurationMS: 5000, StartOffsetMS: 5000, EndOffsetMS: 10000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
	}, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	cfg := config.Default()
	cfg.ComfyUI.Enabled = true
	cfg.ComfyUI.BaseURL = fake.URL
	cfg.ComfyUI.RequestTimeoutSec = 5
	cfg.Database.Path = t.TempDir() + "/w.db"
	wk := New(cfg, st, mediaStore, slog.Default())

	if _, err := wk.GenerateAnchor(t.Context(), plan.Anchors[1], *plan.SuperShot); err != nil {
		t.Fatalf("generate anchor: %v", err)
	}
	graph := fake.LastPrompt()
	if graph == nil {
		t.Fatal("no graph submitted")
	}
	raw, _ := json.Marshal(graph)
	if !strings.Contains(string(raw), "uploaded_") {
		t.Fatalf("the submitted graph must contain a staged reference image, got %s", string(raw))
	}
	// The anchor records its own image attempt.
	fresh, _ := st.GetAnchor(t.Context(), plan.Anchors[1].ID)
	if !fresh.ImageAttemptID.Valid {
		t.Fatal("anchor image attempt must be recorded")
	}
}

// TestAnchorReferencesSelectsEndpointsAndPreviousAnchor proves the reference
// mapping used for intermediate-image generation.
func TestAnchorReferencesSelectsEndpointsAndPreviousAnchor(t *testing.T) {
	st := testutil.NewStore(t)
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Refs2")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	spec, _ := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	plan, err := st.CreateSuperShotPlan(t.Context(), store.SuperShotRequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version, WorkflowVersionID: "w",
		Strategy: "chained", RequestedDurationMS: 15000, PlannedDurationMS: 10000, FPSNum: 8, FPSDen: 1, Width: 512, Height: 320,
	}, []store.AnchorInput{
		{Index: 0, Kind: "supplied_start", StatePrompt: "a", AssetID: "asset-start"},
		{Index: 1, Kind: "intermediate", StatePrompt: "b", TimeOffsetMS: 5000, AssetID: "asset-b"},
		{Index: 2, Kind: "intermediate", StatePrompt: "c", TimeOffsetMS: 8000},
		{Index: 3, Kind: "supplied_end", StatePrompt: "d", TimeOffsetMS: 10000, AssetID: "asset-end"},
	}, []store.SegmentInput{
		{RawAction: "x", DurationMS: 3000, FrameCount: 1, FPSNum: 8, FPSDen: 1},
		{RawAction: "y", DurationMS: 3000, FrameCount: 1, FPSNum: 8, FPSDen: 1},
		{RawAction: "z", DurationMS: 4000, FrameCount: 1, FPSNum: 8, FPSDen: 1},
	}, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	mediaStore := media.New(t.TempDir())
	cfg := config.Default()
	wk := New(cfg, st, mediaStore, slog.Default())
	refs := wk.anchorReferences(t.Context(), plan.Anchors[2], plan.SuperShot)
	if refs["first_frame_asset"] != "asset-start" {
		t.Fatalf("first_frame_asset = %q, want asset-start", refs["first_frame_asset"])
	}
	if refs["last_frame_asset"] != "asset-end" {
		t.Fatalf("last_frame_asset = %q, want asset-end", refs["last_frame_asset"])
	}
	if refs["reference_asset"] != "asset-b" {
		t.Fatalf("reference_asset should be the previous anchor, got %q", refs["reference_asset"])
	}
}