package workflows

import "testing"

func testGraph() map[string]any {
	g, err := CloneGraph(`{
		"1": {"class_type":"CheckpointLoaderSimple","inputs":{"ckpt_name":"a.safetensors"}},
		"2": {"class_type":"CLIPTextEncode","inputs":{"text":"","clip":["1",1]}},
		"7": {"class_type":"SaveImage","inputs":{"images":["6",0]}}
	}`)
	if err != nil {
		panic(err)
	}
	return g
}

func TestApplyBindingsSetsOnlyNamedInputs(t *testing.T) {
	g := testGraph()
	bindings := []Binding{
		{SemanticField: "first_frame_positive", NodeID: "2", InputName: "text", Required: true},
		{SemanticField: "model_name", NodeID: "1", InputName: "ckpt_name"},
		{SemanticField: "output_node", NodeID: "7"},
	}
	out, err := ApplyBindings(g, bindings, map[string]any{
		"first_frame_positive": "a red barn",
		"model_name":           "b.safetensors",
	})
	if err != nil {
		t.Fatal(err)
	}
	node2 := out["2"].(map[string]any)["inputs"].(map[string]any)
	node1 := out["1"].(map[string]any)["inputs"].(map[string]any)
	if node2["text"] != "a red barn" {
		t.Fatalf("text not applied: %v", node2["text"])
	}
	if node1["ckpt_name"] != "b.safetensors" {
		t.Fatalf("ckpt_name not applied: %v", node1["ckpt_name"])
	}
}

func TestApplyBindingsMissingRequiredErrors(t *testing.T) {
	g := testGraph()
	_, err := ApplyBindings(g, []Binding{{SemanticField: "first_frame_positive", NodeID: "2", InputName: "text", Required: true}}, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required binding")
	}
}

func TestApplyBindingsMissingNodeErrors(t *testing.T) {
	g := testGraph()
	_, err := ApplyBindings(g, []Binding{{SemanticField: "seed", NodeID: "99", InputName: "seed", Required: true}}, map[string]any{"seed": 1})
	if err == nil {
		t.Fatal("expected error for missing node (no guessing)")
	}
}

func TestApplyBindingsMissingInputErrors(t *testing.T) {
	g := testGraph()
	_, err := ApplyBindings(g, []Binding{{SemanticField: "width", NodeID: "2", InputName: "width", Required: true}}, map[string]any{"width": 512})
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}

func TestOutputNodeID(t *testing.T) {
	if got := OutputNodeID([]Binding{{SemanticField: "output_node", NodeID: "7"}}); got != "7" {
		t.Fatalf("got %q", got)
	}
	if got := OutputNodeID(nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestBundledManifestLoads(t *testing.T) {
	pkg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Slug != "sdxl-txt2img" {
		t.Fatalf("unexpected slug %q", pkg.Manifest.Slug)
	}
	if pkg.GraphSHA == "" || len(pkg.GraphJSON) == 0 {
		t.Fatal("graph not loaded")
	}
	var hasOutput bool
	for _, b := range pkg.Manifest.Bindings {
		if b.SemanticField == "output_node" {
			hasOutput = true
		}
	}
	if !hasOutput {
		t.Fatal("manifest missing output_node binding")
	}
}

func TestBundledWanPackageLoadsAndBinds(t *testing.T) {
	all, err := LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("expected at least 2 bundled packages, got %d", len(all))
	}
	var wan *Packaged
	for _, p := range all {
		if p.Manifest.Slug == "wan-flf-video" {
			wan = p
		}
	}
	if wan == nil {
		t.Fatal("wan-flf-video package not found")
	}
	if wan.Manifest.Task != "video" {
		t.Fatalf("wan task = %q, want video", wan.Manifest.Task)
	}
	if got := OutputNodeID(wan.Manifest.Bindings); got != "12" {
		t.Fatalf("wan output node = %q, want 12", got)
	}
	graph, err := CloneGraph(wan.GraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{
		"first_frame_positive": "a potter at the wheel",
		"first_frame_negative": "lowres",
		"first_frame_asset":    "start.png",
		"last_frame_asset":     "end.png",
		"width":                512,
		"height":               320,
		"seed":                 7,
		"model_name":           "wan2.2_ti2v_5B_fp16.safetensors",
		"fps":                  8.0,
	}
	if _, err := ApplyBindings(graph, wan.Manifest.Bindings, values); err != nil {
		t.Fatalf("wan bindings did not resolve against the graph: %v", err)
	}
}
