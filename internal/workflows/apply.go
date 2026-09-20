package workflows

import (
	"encoding/json"
	"fmt"
)

// CloneGraph deep-copies a ComfyUI API-format graph via JSON round-trip.
func CloneGraph(graphJSON string) (map[string]any, error) {
	var g map[string]any
	if err := json.Unmarshal([]byte(graphJSON), &g); err != nil {
		return nil, fmt.Errorf("workflows: parse graph: %w", err)
	}
	return g, nil
}

// ApplyBindings sets only the confirmed semantic bindings on a cloned graph.
// It never guesses: a missing node or input is an error (spec §9, §27).
func ApplyBindings(graph map[string]any, bindings []Binding, values map[string]any) (map[string]any, error) {
	for _, b := range bindings {
		if b.SemanticField == "output_node" {
			continue
		}
		value, ok := values[b.SemanticField]
		if !ok {
			if b.Required {
				return nil, fmt.Errorf("workflows: missing required binding value %q", b.SemanticField)
			}
			continue
		}
		node, ok := graph[b.NodeID].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("workflows: bound node %q not present in graph", b.NodeID)
		}
		inputs, ok := node["inputs"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("workflows: node %q has no inputs", b.NodeID)
		}
		if _, exists := inputs[b.InputName]; !exists {
			return nil, fmt.Errorf("workflows: node %q input %q not present", b.NodeID, b.InputName)
		}
		inputs[b.InputName] = value
	}
	return graph, nil
}

// OutputNodeID returns the node id marked as the output binding.
func OutputNodeID(bindings []Binding) string {
	for _, b := range bindings {
		if b.SemanticField == "output_node" {
			return b.NodeID
		}
	}
	return ""
}
