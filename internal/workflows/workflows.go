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
	"sort"
	"strings"
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

// Load reads the single default bundled workflow package (the SDXL text-to-image
// package). Kept for callers that expect one default; use LoadAll to seed every
// bundled package.
func Load() (*Packaged, error) {
	all, err := LoadAll()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.Manifest.Slug == "sdxl-txt2img" {
			return p, nil
		}
	}
	return all[0], nil
}

// LoadAll reads every bundled workflow package (manifests named *manifest*.json),
// ordered by slug for deterministic seeding.
func LoadAll() ([]*Packaged, error) {
	entries, err := fs.ReadDir(bundled, "bundled")
	if err != nil {
		return nil, fmt.Errorf("workflows: read bundled dir: %w", err)
	}
	var out []*Packaged
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || !strings.Contains(e.Name(), "manifest") {
			continue
		}
		pkg, err := loadOne("bundled/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, pkg)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("workflows: no bundled manifests found")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Slug < out[j].Manifest.Slug })
	return out, nil
}

func loadOne(manifestPath string) (*Packaged, error) {
	raw, err := fs.ReadFile(bundled, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("workflows: read manifest %s: %w", manifestPath, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("workflows: parse manifest %s: %w", manifestPath, err)
	}
	if m.GraphAPIFile == "" {
		return nil, fmt.Errorf("workflows: manifest %s has no graph_api_file", manifestPath)
	}
	graph, err := fs.ReadFile(bundled, "bundled/"+m.GraphAPIFile)
	if err != nil {
		return nil, fmt.Errorf("workflows: read graph %s: %w", m.GraphAPIFile, err)
	}
	sum := sha256.Sum256(graph)
	return &Packaged{Manifest: m, ManifestJSON: string(raw), GraphJSON: string(graph), GraphSHA: hex.EncodeToString(sum[:])}, nil
}
