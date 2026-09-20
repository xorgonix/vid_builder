package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/store"
	"vidbuild/internal/templates"
)

// knownSemanticFields is the vocabulary vid-build may bind.
var knownSemanticFields = []string{
	"first_frame_positive", "first_frame_negative",
	"last_frame_positive", "last_frame_negative",
	"motion_positive", "motion_negative",
	"first_frame_asset", "last_frame_asset", "reference_asset",
	"seed", "width", "height", "frame_count", "fps", "model_name", "lora_stack",
	"output_node",
}

func (s *Server) handleWorkflowImport(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	user := UserFrom(r.Context())
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Redirect(w, r, "/w/"+ws.ID+"/workflows?msg="+urlMsg("Could not read upload.")+"&kind=error", http.StatusSeeOther)
			return
		}
	} else {
		_ = r.ParseForm()
	}
	graphJSON := r.FormValue("graph_json")
	if f, _, err := r.FormFile("graph"); err == nil {
		defer f.Close()
		raw, _ := io.ReadAll(io.LimitReader(f, 32<<20))
		if len(strings.TrimSpace(string(raw))) > 0 {
			graphJSON = string(raw)
		}
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Imported workflow"
	}
	task := formValueOr(r, "task", "image")

	// Validate JSON and that it looks like an API-format graph.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(graphJSON), &parsed); err != nil {
		http.Redirect(w, r, "/w/"+ws.ID+"/workflows?msg="+urlMsg("Import failed: not valid JSON.")+"&kind=error", http.StatusSeeOther)
		return
	}
	nodeCount := 0
	for _, v := range parsed {
		if _, ok := v.(map[string]any); ok {
			nodeCount++
		}
	}
	if nodeCount == 0 {
		http.Redirect(w, r, "/w/"+ws.ID+"/workflows?msg="+urlMsg("Import failed: this does not look like a ComfyUI API-format graph.")+"&kind=error", http.StatusSeeOther)
		return
	}

	sum := sha256.Sum256([]byte(graphJSON))
	graphSHA := hex.EncodeToString(sum[:])
	manifest := map[string]any{"name": name, "task": task, "imported": true, "node_count": nodeCount}
	manifestJSON, _ := json.Marshal(manifest)
	bindings := suggestBindings(parsed)

	pkg, ver, err := s.store.CreateImportedWorkflow(r.Context(), ws.ID, user.ID, name, slugFor(name), task, graphJSON, graphSHA, string(manifestJSON), bindings)
	if err != nil {
		s.log.Error("import workflow", "err", err)
		http.Redirect(w, r, "/w/"+ws.ID+"/workflows?msg="+urlMsg("Import failed.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "workflow.import", "workflow_version", ver.ID, "imported workflow", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+ver.ID+"?msg="+urlMsg("Imported "+pkg.Name+". Confirm bindings, then validate.")+"&kind=success", http.StatusSeeOther)
}

// suggestBindings heuristically maps common node types to semantic fields. The
// human must confirm them before the version can be validated (spec §8.5).
func suggestBindings(graph map[string]any) []store.WorkflowBinding {
	ids := make([]string, 0, len(graph))
	for id := range graph {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return numericLess(ids[i], ids[j])
	})
	var out []store.WorkflowBinding
	textCount := 0
	imageCount := 0
	for _, id := range ids {
		node, ok := graph[id].(map[string]any)
		if !ok {
			continue
		}
		class, _ := node["class_type"].(string)
		switch class {
		case "CheckpointLoaderSimple":
			out = append(out, store.WorkflowBinding{SemanticField: "model_name", NodeID: id, InputName: "ckpt_name", ValueType: "string"})
		case "CLIPTextEncode":
			textCount++
			field := "first_frame_positive"
			if textCount == 2 {
				field = "first_frame_negative"
			}
			out = append(out, store.WorkflowBinding{SemanticField: field, NodeID: id, InputName: "text", ValueType: "string"})
		case "LoadImage":
			imageCount++
			field := "first_frame_asset"
			if imageCount == 2 {
				field = "last_frame_asset"
			} else if imageCount > 2 {
				field = "reference_asset"
			}
			out = append(out, store.WorkflowBinding{SemanticField: field, NodeID: id, InputName: "image", ValueType: "string"})
		case "EmptyLatentImage":
			out = append(out,
				store.WorkflowBinding{SemanticField: "width", NodeID: id, InputName: "width", ValueType: "int"},
				store.WorkflowBinding{SemanticField: "height", NodeID: id, InputName: "height", ValueType: "int"})
		case "KSampler", "KSamplerAdvanced":
			out = append(out, store.WorkflowBinding{SemanticField: "seed", NodeID: id, InputName: "seed", ValueType: "int"})
		case "SaveImage", "PreviewImage":
			out = append(out, store.WorkflowBinding{SemanticField: "output_node", NodeID: id, InputName: "", ValueType: "node", Required: true})
		}
	}
	return out
}

func numericLess(a, b string) bool {
	ai, aerr := parseFloat(a)
	bi, berr := parseFloat(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

func parseFloat(s string) (float64, error) {
	var f float64
	if err := json.Unmarshal([]byte(s), &f); err == nil {
		return f, nil
	}
	return 0, errNotNumber
}

var errNotNumber = &numErr{}

type numErr struct{}

func (*numErr) Error() string { return "not a number" }

func slugFor(name string) string {
	out := strings.Builder{}
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && out.Len() > 0 {
				out.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.Trim(out.String(), "-")
	if s == "" {
		return "workflow"
	}
	return s
}

func (s *Server) handleWorkflowVersion(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "Workflow version")
	shell.ActiveNav = "workflows"
	versionID := chiURLParam(r, "versionID")
	ver, err := s.store.GetWorkflowVersion(r.Context(), versionID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	pkg, err := s.store.GetWorkflowPackage(r.Context(), ver.WorkflowPackageID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if pkg.WorkspaceID.Valid && pkg.WorkspaceID.String != ws.ID {
		s.forbidden(w, r)
		return
	}
	bindings, _ := s.store.ListBindings(r.Context(), ver.ID)
	var graph map[string]any
	_ = json.Unmarshal([]byte(ver.GraphJSON), &graph)

	// Detected node classes.
	classCounts := map[string]int{}
	for _, v := range graph {
		if node, ok := v.(map[string]any); ok {
			if class, ok := node["class_type"].(string); ok {
				classCounts[class]++
			}
		}
	}
	classes := make([]string, 0, len(classCounts))
	for c := range classCounts {
		classes = append(classes, c)
	}
	sort.Strings(classes)

	data := templates.WorkflowVersionData{
		Shell:           shell,
		PackageID:       pkg.ID,
		PackageName:     pkg.Name,
		Source:          pkg.Source,
		VersionID:       ver.ID,
		Status:          ver.Status,
		GraphSHA:        ver.GraphSHA,
		SemanticVersion: ver.SemanticVersion,
		NodeCount:       len(graph),
		Classes:         []templates.NodeClassCount{},
		Bindings:        []templates.BindingRow{},
		AllFields:       knownSemanticFields,
	}
	for _, c := range classes {
		data.Classes = append(data.Classes, templates.NodeClassCount{Class: c, Count: classCounts[c]})
	}
	for _, b := range bindings {
		data.Bindings = append(data.Bindings, templates.BindingRow{Field: b.SemanticField, NodeID: b.NodeID, InputName: b.InputName, Required: b.Required})
		data.BoundFields = append(data.BoundFields, b.SemanticField)
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.WorkflowVersionPage(data))
}

func (s *Server) handleWorkflowBindings(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	versionID := chiURLParam(r, "versionID")
	ver, err := s.store.GetWorkflowVersion(r.Context(), versionID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = r.ParseForm()
	var bindings []store.WorkflowBinding
	for _, field := range r.Form["field"] {
		nodeID := r.FormValue("node_" + field)
		inputName := r.FormValue("input_" + field)
		if strings.TrimSpace(nodeID) == "" {
			continue
		}
		bindings = append(bindings, store.WorkflowBinding{
			SemanticField: field, NodeID: strings.TrimSpace(nodeID), InputName: strings.TrimSpace(inputName),
			ValueType: "string", Required: r.FormValue("required_"+field) != "",
		})
	}
	if err := s.store.ReplaceBindings(r.Context(), versionID, bindings); err != nil {
		s.log.Error("replace bindings", "err", err)
		http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+versionID+"?msg="+urlMsg("Could not save bindings.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.SetWorkflowVersionStatus(r.Context(), versionID, "draft")
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "workflow.bindings", "workflow_version", versionID, "confirmed bindings", "{}")
	_ = ver
	http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+versionID+"?msg="+urlMsg("Bindings saved. Run validation against the target.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleWorkflowValidate(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	versionID := chiURLParam(r, "versionID")
	ver, err := s.store.GetWorkflowVersion(r.Context(), versionID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	bindings, _ := s.store.ListBindings(r.Context(), ver.ID)
	var graph map[string]any
	_ = json.Unmarshal([]byte(ver.GraphJSON), &graph)

	problems := []string{}

	// 1. Every bound node/input must exist in the graph (never guess).
	for _, b := range bindings {
		node, ok := graph[b.NodeID].(map[string]any)
		if !ok {
			problems = append(problems, "binding "+b.SemanticField+": node "+b.NodeID+" missing")
			continue
		}
		if b.SemanticField == "output_node" {
			continue
		}
		inputs, _ := node["inputs"].(map[string]any)
		if _, ok := inputs[b.InputName]; !ok {
			problems = append(problems, "binding "+b.SemanticField+": input "+b.InputName+" missing on node "+b.NodeID)
		}
	}
	// Required semantic fields.
	have := map[string]bool{}
	for _, b := range bindings {
		have[b.SemanticField] = true
	}
	if !have["output_node"] {
		problems = append(problems, "no output_node binding")
	}

	// 2. Node classes and checkpoints must exist on the running ComfyUI.
	client := comfyui.New(s.cfg.ComfyUI)
	if _, err := client.Stats(r.Context()); err != nil {
		problems = append(problems, "ComfyUI unreachable: "+err.Error())
	} else {
		seen := map[string]bool{}
		for _, v := range graph {
			node, ok := v.(map[string]any)
			if !ok {
				continue
			}
			class, _ := node["class_type"].(string)
			if class == "" || seen[class] {
				continue
			}
			seen[class] = true
			if ok, err := client.HasNode(r.Context(), class); err != nil || !ok {
				problems = append(problems, "missing node type: "+class)
			}
		}
		if cps, err := client.Checkpoints(r.Context()); err == nil {
			available := map[string]bool{}
			for _, c := range cps {
				available[c] = true
			}
			for _, v := range graph {
				node, _ := v.(map[string]any)
				if node == nil {
					continue
				}
				if class, _ := node["class_type"].(string); class == "CheckpointLoaderSimple" {
					if inputs, ok := node["inputs"].(map[string]any); ok {
						if cp, ok := inputs["ckpt_name"].(string); ok && cp != "" && !available[cp] {
							problems = append(problems, "missing checkpoint: "+cp)
						}
					}
				}
			}
		}
	}

	if len(problems) > 0 {
		_ = s.store.SetWorkflowVersionStatus(r.Context(), versionID, "invalid")
		http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+versionID+"?msg="+urlMsg("Validation failed: "+strings.Join(problems, "; "))+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.SetWorkflowVersionStatus(r.Context(), versionID, "validated")
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "workflow.validate", "workflow_version", versionID, "validated", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+versionID+"?msg="+urlMsg("Validation passed: graph, bindings and target capabilities agree.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleWorkflowActivate(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	versionID := chiURLParam(r, "versionID")
	ver, err := s.store.GetWorkflowVersion(r.Context(), versionID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if ver.Status != "validated" {
		http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+versionID+"?msg="+urlMsg("Only a validated version can be activated.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.SetWorkflowVersionStatus(r.Context(), versionID, "validated")
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "workflow.activate", "workflow_version", versionID, "activated", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+versionID+"?msg="+urlMsg("Workflow version is active and selectable.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleWorkflowRetire(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	versionID := chiURLParam(r, "versionID")
	_ = s.store.SetWorkflowVersionStatus(r.Context(), versionID, "retired")
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "workflow.retire", "workflow_version", versionID, "retired", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/workflow-versions/"+versionID+"?msg="+urlMsg("Version retired; existing receipts are preserved.")+"&kind=info", http.StatusSeeOther)
}
