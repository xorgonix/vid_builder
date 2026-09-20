// Package workflows embeds bundled, human-authored ComfyUI workflow packages.
// Graphs are data authored in ComfyUI; Go only stores and binds them (spec §12.8).
package workflows

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
)

//go:embed bundled/*
var bundled embed.FS

// Binding maps a vid-build semantic field to a node input.
type Binding struct {
	SemanticField string `json:"semantic_field"`
	NodeID        string `json:"node_id"`
	InputName     string `json:"input_name"`
	ValueType     string `json:"value_type"`
	Required      bool   `json:"required"`
}

// ExpectedOutput declares an output node.
type ExpectedOutput struct {
	NodeID string `json:"node_id"`
	Type   string `json:"type"`
	Field  string `json:"field"`
}

// Manifest describes a bundled package.
type Manifest struct {
	Name                 string              `json:"name"`
	Slug                 string              `json:"slug"`
	Task                 string              `json:"task"`
	SemanticVersion      string              `json:"semantic_version"`
	TestedComfyUIVersion string              `json:"tested_comfyui_version"`
	Description          string              `json:"description"`
	GraphAPIFile         string              `json:"graph_api_file"`
	GraphUIFile          string              `json:"graph_ui_file"`
	Bindings             []Binding           `json:"bindings"`
	Requirements         map[string][]string `json:"requirements"`
	ExpectedOutputs      []ExpectedOutput    `json:"expected_outputs"`
}

// Packaged is a loaded bundled workflow.
type Packaged struct {
	Manifest     Manifest
	ManifestJSON string
	GraphJSON    string
	GraphSHA     string
}

// Load reads the single bundled default workflow package.
func Load() (*Packaged, error) {
	raw, err := fs.ReadFile(bundled, "bundled/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("workflows: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("workflows: parse manifest: %w", err)
	}
	graphFile := m.GraphAPIFile
	if graphFile == "" {
		graphFile = "sdxl_txt2img_api.json"
	}
	graph, err := fs.ReadFile(bundled, "bundled/"+graphFile)
	if err != nil {
		return nil, fmt.Errorf("workflows: read graph: %w", err)
	}
	sum := sha256.Sum256(graph)
	return &Packaged{Manifest: m, ManifestJSON: string(raw), GraphJSON: string(graph), GraphSHA: hex.EncodeToString(sum[:])}, nil
}
