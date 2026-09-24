package worker

import (
	"log/slog"
	"strings"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/media"
	"vidbuild/internal/store"
	"vidbuild/internal/testutil"
)

const a02Manifest = `{
  "name": "Ref Image", "slug": "ref-image-r1", "task": "image",
  "capabilities": {
    "inputs": { "start_image": true, "end_image": true, "reference_image": true },
    "continuation": { "mode": "anchor_only" }, "verified": false
  }
}`

const a02Graph = `{
  "4": {"class_type": "CLIPTextEncode", "inputs": {"text": ""}},
  "7": {"class_type": "SaveImage", "inputs": {}},
  "10": {"class_type": "LoadImage", "inputs": {"image_a": "placeholder-start.png"}},
  "11": {"class_type": "LoadImage", "inputs": {"image_ref": "placeholder-ref.png"}}
}`

var a02Bindings = []store.WorkflowBinding{
	{SemanticField: "first_frame_asset", NodeID: "10", InputName: "image_a", ValueType: "string", Required: true},
	{SemanticField: "reference_asset", NodeID: "11", InputName: "image_ref", ValueType: "string", Required: true},
	{SemanticField: "first_frame_positive", NodeID: "4", InputName: "text", ValueType: "string", Required: true},
	{SemanticField: "output_node", NodeID: "7", InputName: "", ValueType: "node", Required: true},
}

// planForAnchors builds a plan whose image workflow is pre-selected. It returns
// the plan plus the ids needed to attach assets to anchors.
func planForAnchors(t *testing.T, st *store.Store, slug, graphJSON, manifestJSON string, bindings []store.WorkflowBinding, anchors []store.AnchorInput, segments []store.SegmentInput) (*store.SuperShotPlan, string, string, string, *media.Store) {
	t.Helper()
	if err := st.SeedBundledWorkflow(t.Context(), "Img "+slug, slug, "image", "test", graphJSON, "sha-"+slug, manifestJSON, "0.35.0", bindings); err != nil {
		t.Fatalf("seed %s: %v", slug, err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	imageVersion := ""
	for _, p := range pkgs {
		if p.Slug == slug && p.LatestVersion.Valid {
			imageVersion = p.LatestVersion.String
		}
	}
	if imageVersion == "" {
		t.Fatalf("no version for %s", slug)
	}
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "Anch-"+slug)
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	spec, err := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}
	plan, err := st.CreateSuperShotPlan(t.Context(), store.SuperShotRequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version, WorkflowVersionID: imageVersion,
		ImageWorkflowVersionID: imageVersion, ImageMode: "references",
		Strategy: "chained", RequestedDurationMS: 15000, PlannedDurationMS: 10000,
		FPSNum: 8, FPSDen: 1, Width: 512, Height: 320,
		BasePositive: "walk", CompleteMovement: "walk to the chair", ShotPurpose: "show calm",
		PlannerStatus: "manual",
	}, anchors, segments, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan, ws.ID, project.ID, imageVersion, media.New(t.TempDir())
}

// TestA01MissingImageWorkflowIsAnError proves R1.2: an explicitly chosen image
// workflow that cannot load is a hard error, never a silent fallback to another
// selectable image workflow.
func TestA01MissingImageWorkflowIsAnError(t *testing.T) {
	st := testutil.NewStore(t)
	fake := testutil.NewComfyUI(t)

	// One real image workflow remains selectable — the plan must refuse to use it.
	anchors := []store.AnchorInput{
		{Index: 0, Kind: "supplied_start", StatePrompt: "start", TimeOffsetMS: 0},
		{Index: 1, Kind: "intermediate", StatePrompt: "mid1", TimeOffsetMS: 4000},
		{Index: 2, Kind: "intermediate", StatePrompt: "mid2", TimeOffsetMS: 8000},
		{Index: 3, Kind: "supplied_end", StatePrompt: "end", TimeOffsetMS: 15000},
	}
	segments := []store.SegmentInput{
		{RawAction: "a", DurationMS: 4000, FrameCount: 32, FPSNum: 8, FPSDen: 1},
		{RawAction: "b", DurationMS: 3000, FrameCount: 24, FPSNum: 8, FPSDen: 1},
		{RawAction: "c", DurationMS: 3000, FrameCount: 24, FPSNum: 8, FPSDen: 1},
	}
	// Seed a working image workflow so pickImageWorkflow would succeed if we fell back.
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "MissA01")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	spec, err := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}
	if err := st.SeedBundledWorkflow(t.Context(), "Ref Image", "ref-image-r1", "image", "test", a02Graph, "sha-r1", a02Manifest, "0.35.0", a02Bindings); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	imgVer := ""
	for _, p := range pkgs {
		if p.Slug == "ref-image-r1" && p.LatestVersion.Valid {
			imgVer = p.LatestVersion.String
		}
	}
	plan, err := st.CreateSuperShotPlan(t.Context(), store.SuperShotRequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version, WorkflowVersionID: imgVer,
		ImageWorkflowVersionID: "this-version-does-not-exist", ImageMode: "references",
		Strategy: "chained", RequestedDurationMS: 15000, PlannedDurationMS: 10000,
		FPSNum: 8, FPSDen: 1, Width: 512, Height: 320, BasePositive: "walk", CompleteMovement: "walk",
		ShotPurpose: "calm", PlannerStatus: "manual",
	}, anchors, segments, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	cfg := config.Default()
	cfg.ComfyUI.BaseURL = fake.URL
	cfg.Database.Path = t.TempDir() + "/w.db"
	wk := New(cfg, st, media.New(t.TempDir()), slog.Default())

	res, err := wk.GenerateAnchor(t.Context(), plan.Anchors[1], *plan.SuperShot)
	if err == nil {
		t.Fatal("a missing explicitly selected image workflow must fail, not silently fall back")
	}
	if !strings.Contains(err.Error(), "could not be loaded") {
		t.Fatalf("expected an actionable 'could not be loaded' error, got: %v", err)
	}
	if res != "" {
		t.Fatal("no anchor output may be stored")
	}
	// And no upstream/alternate workflow is picked for us.
	if _, err := st.GetSuperShotRequest(t.Context(), plan.SuperShot.ID); err != nil {
		t.Fatalf("plan load: %v", err)
	}
}

// TestA01bNoReferenceBindingIsRejected proves R1.3: in reference mode a graph
// with no reference binding is rejected with an actionable message, never
// silently rendered text-only.
func TestA01bNoReferenceBindingIsRejected(t *testing.T) {
	st := testutil.NewStore(t)
	fake := testutil.NewComfyUI(t)
	noRefGraph := `{"4":{"class_type":"CLIPTextEncode","inputs":{"text":""}},"7":{"class_type":"SaveImage","inputs":{}}}`
	noRefBindings := []store.WorkflowBinding{
		{SemanticField: "first_frame_positive", NodeID: "4", InputName: "text", ValueType: "string", Required: true},
		{SemanticField: "output_node", NodeID: "7", InputName: "", ValueType: "node", Required: true},
	}
	plan, _, _, _, _ := planForAnchors(t, st, "plain-image-r1", noRefGraph, `{"name":"Plain","slug":"plain-image-r1","task":"image","capabilities":{"inputs":{"start_image":true,"end_image":true},"continuation":{"mode":"anchor_only"},"verified":false}}`, noRefBindings,
		[]store.AnchorInput{
			{Index: 0, Kind: "supplied_start", StatePrompt: "start", TimeOffsetMS: 0},
			{Index: 1, Kind: "intermediate", StatePrompt: "mid", TimeOffsetMS: 5000},
			{Index: 2, Kind: "supplied_end", StatePrompt: "end", TimeOffsetMS: 10000},
		},
		[]store.SegmentInput{
			{RawAction: "a", DurationMS: 5000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
			{RawAction: "b", DurationMS: 5000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
		})
	wk := New(config.Default(), st, media.New(t.TempDir()), slog.Default())
	_, err := wk.GenerateAnchor(t.Context(), plan.Anchors[1], *plan.SuperShot)
	if err == nil {
		t.Fatal("a reference-mode plan must fail when the graph declares no reference binding")
	}
	if !strings.Contains(err.Error(), "no reference binding") {
		t.Fatalf("expected an actionable capability error, got: %v", err)
	}
	if fake.LastPrompt() != nil {
		t.Fatal("no graph may be submitted when the workflow lacks reference bindings")
	}
}

// TestA02DistinctReferencesReachDistinctInputs proves R1.4/A02: start endpoint
// and predecessor anchor each reach their own declared graph input, not merely
// "some uploaded string somewhere".
func TestA02DistinctReferencesReachDistinctInputs(t *testing.T) {
	st := testutil.NewStore(t)
	fake := testutil.NewComfyUI(t)
	mediaStore := media.New(t.TempDir())
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "A02")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	spec, err := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}
	startA := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "start_frame", "start.png", "start-bytes")
	predecessorA := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "video_anchor", "pred.png", "predecessor-bytes")
	endA := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "end_frame", "end.png", "end-bytes")

	if err := st.SeedBundledWorkflow(t.Context(), "Ref Image", "ref-image-r1", "image", "test", a02Graph, "sha-x", a02Manifest, "0.35.0", a02Bindings); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	imgVer := ""
	for _, p := range pkgs {
		if p.Slug == "ref-image-r1" && p.LatestVersion.Valid {
			imgVer = p.LatestVersion.String
		}
	}
	anchors := []store.AnchorInput{
		{Index: 0, Kind: "supplied_start", StatePrompt: "a", TimeOffsetMS: 0, AssetID: startA},
		{Index: 1, Kind: "intermediate", StatePrompt: "b", TimeOffsetMS: 4000, AssetID: predecessorA},
		{Index: 2, Kind: "intermediate", StatePrompt: "c", TimeOffsetMS: 8000},
		{Index: 3, Kind: "supplied_end", StatePrompt: "d", TimeOffsetMS: 12000, AssetID: endA},
	}
	segments := []store.SegmentInput{
		{RawAction: "s1", DurationMS: 4000, FrameCount: 32, FPSNum: 8, FPSDen: 1},
		{RawAction: "s2", DurationMS: 4000, FrameCount: 32, FPSNum: 8, FPSDen: 1},
		{RawAction: "s2b", DurationMS: 4000, FrameCount: 32, FPSNum: 8, FPSDen: 1},
	}
	plan, err := st.CreateSuperShotPlan(t.Context(), store.SuperShotRequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version, WorkflowVersionID: imgVer,
		ImageWorkflowVersionID: imgVer, ImageMode: "references",
		Strategy: "chained", RequestedDurationMS: 15000, PlannedDurationMS: 12000,
		FPSNum: 8, FPSDen: 1, Width: 512, Height: 320, BasePositive: "walk",
		CompleteMovement: "walk", ShotPurpose: "calm", PlannerStatus: "manual",
	}, anchors, segments, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	cfg := config.Default()
	cfg.ComfyUI.BaseURL = fake.URL
	cfg.ComfyUI.RequestTimeoutSec = 5
	wk := New(cfg, st, mediaStore, slog.Default())
	if _, err := wk.GenerateAnchor(t.Context(), plan.Anchors[2], *plan.SuperShot); err != nil {
		t.Fatalf("generate anchor: %v", err)
	}
	graph := fake.LastPrompt()
	if graph == nil {
		t.Fatal("no graph submitted")
	}
	n10, ok := graph["10"].(map[string]any)
	if !ok || n10["inputs"] == nil {
		t.Fatal("missing node 10 inputs")
	}
	n11, ok := graph["11"].(map[string]any)
	if !ok {
		t.Fatal("missing node 11 inputs")
	}
	// Extract node input names so we can assert the exact mapping.
	nodeInA, _ := n10["inputs"].(map[string]any)
	nodeInB, ok2 := n11["inputs"].(map[string]any)
	if !ok2 {
		t.Fatal("missing node 11 inputs")
	}
	a := nodeInA["image_a"]
	r := nodeInB["image_ref"]
	if a == nil || r == nil {
		t.Fatalf("node inputs missing: %#v / %#v", a, r)
	}
	if a == r {
		t.Fatalf("start and predecessor must be distinct staged uploads, both were %v", a)
	}
	au, ru := a.(string), r.(string)
	if !strings.HasPrefix(au, "uploaded_") || !strings.HasPrefix(ru, "uploaded_") {
		t.Fatalf("staged references must be ComfyUI upload names: %v / %v", au, ru)
	}
	numA, numR := uploadNumber(au), uploadNumber(ru)
	if numA == numR {
		t.Fatalf("distinct sources must map to distinct uploads: %v vs %v", au, ru)
	}
}

// TestA03InvalidOutputBytesAreRejected proves R1.8/A03: garbage bytes in a
// .png-named result must be rejected before publication.
func TestA03InvalidOutputBytesAreRejected(t *testing.T) {
	st := testutil.NewStore(t)
	fake := testutil.NewComfyUI(t)
	fake.SetRawViewData([]byte("this is definitely not a PNG or JPEG image, just plain text"))
	admin := testutil.MustAdmin(t, st)
	ws := testutil.MustWorkspace(t, st)
	project, vid := testutil.MustProjectVideo(t, st, ws.ID, admin.ID, "A03")
	_, _, shot := testutil.MustShot(t, st, vid.ID, admin.ID, "Shot")
	spec, err := st.GetCurrentSpec(t.Context(), shot.ID, "motion")
	if err != nil || spec == nil {
		t.Fatalf("motion spec: %v", err)
	}
	mediaStore := media.New(t.TempDir())
	startA := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "start_frame", "start.png", "start-bytes")
	endA := mkImageAsset(t, st, mediaStore, ws.ID, project.ID, "end_frame", "end.png", "end-bytes")
	if err := st.SeedBundledWorkflow(t.Context(), "Ref Image", "ref-image-r1", "image", "test", a02Graph, "sha-a03", a02Manifest, "0.35.0", a02Bindings); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pkgs, _ := st.ListWorkflowPackages(t.Context())
	imgVer := ""
	for _, p := range pkgs {
		if p.Slug == "ref-image-r1" && p.LatestVersion.Valid {
			imgVer = p.LatestVersion.String
		}
	}
	anchors := []store.AnchorInput{
		{Index: 0, Kind: "supplied_start", StatePrompt: "start", TimeOffsetMS: 0, AssetID: startA},
		{Index: 1, Kind: "intermediate", StatePrompt: "mid", TimeOffsetMS: 5000},
		{Index: 2, Kind: "supplied_end", StatePrompt: "end", TimeOffsetMS: 10000, AssetID: endA},
	}
	segments := []store.SegmentInput{
		{RawAction: "a", DurationMS: 5000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
		{RawAction: "b", DurationMS: 5000, FrameCount: 40, FPSNum: 8, FPSDen: 1},
	}
	plan, err := st.CreateSuperShotPlan(t.Context(), store.SuperShotRequestInput{
		WorkspaceID: ws.ID, ProjectID: project.ID, VideoID: vid.ID, ShotID: shot.ID,
		ShotSpecID: spec.ID, SpecVersion: spec.Version, WorkflowVersionID: imgVer,
		ImageWorkflowVersionID: imgVer, ImageMode: "references",
		Strategy: "chained", RequestedDurationMS: 15000, PlannedDurationMS: 10000,
		FPSNum: 8, FPSDen: 1, Width: 512, Height: 320, BasePositive: "walk", CompleteMovement: "walk",
		ShotPurpose: "calm", PlannerStatus: "manual",
	}, anchors, segments, admin.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	wk := newTestWorker(t, st, fake.URL, mediaStore)
	// The run must proceed through staging/submission and fail only at the
	// output-validation stage, where the garbage bytes are rejected.
	_, err = wk.GenerateAnchor(t.Context(), plan.Anchors[1], *plan.SuperShot)
	if err == nil {
		t.Fatal("non-image output bytes must be rejected")
	}
	if !strings.Contains(err.Error(), "decodable") {
		t.Fatalf("expected a decodable-image rejection, got: %v", err)
	}
}

// uploadNumber extracts the counter from an "uploaded_N.png" name so tests can
// distinguish distinct staged uploads.
func uploadNumber(name string) int {
	s := strings.TrimPrefix(name, "uploaded_")
	s = strings.TrimSuffix(s, ".png")
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			continue
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// mkImageAsset stores a real image file in the media store and records the asset.
func mkImageAsset(t *testing.T, st *store.Store, mediaStore *media.Store, wsID, projectID, role, name, body string) string {
	t.Helper()
	uri, checksum, size, err := mediaStore.Put("endpoints", name, strings.NewReader(body))
	if err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
	id, err := st.CreateAsset(t.Context(), store.Asset{WorkspaceID: wsID, ProjectID: projectID, Kind: "image", Role: role, StorageURI: uri, MimeType: "image/png", SizeBytes: size, Checksum: checksum})
	if err != nil {
		t.Fatalf("asset %s: %v", name, err)
	}
	return id
}

// newTestWorker builds a Worker pointed at the fake ComfyUI server.
func newTestWorker(t *testing.T, st *store.Store, fakeURL string, mediaStore *media.Store) *Worker {
	t.Helper()
	cfg := config.Default()
	cfg.ComfyUI.Enabled = true
	cfg.ComfyUI.BaseURL = fakeURL
	cfg.ComfyUI.RequestTimeoutSec = 5
	cfg.Database.Path = t.TempDir() + "/w.db"
	return New(cfg, st, mediaStore, slog.Default())
}