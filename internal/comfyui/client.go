// Package comfyui is the ComfyUI adapter client (spec §18). It talks to a
// private loopback ComfyUI over the documented HTTP API: it submits a concrete
// graph, polls history, and fetches outputs. It never builds node graphs.
package comfyui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vidbuild/internal/config"
)

// Client talks to one ComfyUI instance.
type Client struct {
	cfg  config.ComfyUIConfig
	http *http.Client
}

// New constructs a client.
func New(cfg config.ComfyUIConfig) *Client {
	timeout := time.Duration(cfg.RequestTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: timeout}}
}

// SystemStats is the ComfyUI /system_stats response.
type SystemStats struct {
	System struct {
		ComfyUIVersion string `json:"comfyui_version"`
		PythonVersion  string `json:"python_version"`
	} `json:"system"`
	Devices []struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		VRAMTotal int64  `json:"vram_total"`
		VRAMFree  int64  `json:"vram_free"`
	} `json:"devices"`
}

// CheckpointEntry is one available checkpoint.
type CheckpointEntry struct {
	Name string
}

// Stats fetches system statistics.
func (c *Client) Stats(ctx context.Context) (*SystemStats, error) {
	var out SystemStats
	if err := c.getJSON(ctx, "/system_stats", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Checkpoints lists installed checkpoints via object_info.
func (c *Client) Checkpoints(ctx context.Context) ([]string, error) {
	var oi map[string]struct {
		Input struct {
			Required map[string][]json.RawMessage `json:"required"`
		} `json:"input"`
	}
	if err := c.getJSON(ctx, "/object_info/CheckpointLoaderSimple", &oi); err != nil {
		return nil, err
	}
	entry, ok := oi["CheckpointLoaderSimple"]
	if !ok {
		return nil, fmt.Errorf("comfyui: CheckpointLoaderSimple not reported")
	}
	raw, ok := entry.Input.Required["ckpt_name"]
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("comfyui: ckpt_name not reported")
	}
	var names []string
	if err := json.Unmarshal(raw[0], &names); err != nil {
		return nil, fmt.Errorf("comfyui: decode ckpt_name: %w", err)
	}
	return names, nil
}

// HasNode reports whether a node class exists on the server.
func (c *Client) HasNode(ctx context.Context, classType string) (bool, error) {
	var oi map[string]json.RawMessage
	if err := c.getJSON(ctx, "/object_info/"+url.PathEscape(classType), &oi); err != nil {
		return false, err
	}
	_, ok := oi[classType]
	return ok, nil
}

// PromptResponse is returned by POST /prompt.
type PromptResponse struct {
	PromptID   string         `json:"prompt_id"`
	Number     int            `json:"number"`
	NodeErrors map[string]any `json:"node_errors"`
	Error      any            `json:"error"`
}

// Submit posts a concrete graph and returns the prompt id.
func (c *Client) Submit(ctx context.Context, graph map[string]any, clientID string) (*PromptResponse, error) {
	body, err := json.Marshal(map[string]any{"prompt": graph, "client_id": clientID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/prompt", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comfyui: submit: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comfyui: submit failed: %s: %s", resp.Status, truncate(string(raw), 500))
	}
	var out PromptResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("comfyui: decode prompt response: %w", err)
	}
	if len(out.NodeErrors) > 0 {
		return &out, fmt.Errorf("comfyui: node errors: %s", truncate(string(raw), 500))
	}
	if out.PromptID == "" {
		return &out, fmt.Errorf("comfyui: empty prompt id: %s", truncate(string(raw), 300))
	}
	return &out, nil
}

// OutputImage is one produced image.
type OutputImage struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

// HistoryEntry is one prompt's history record.
type HistoryEntry struct {
	Status struct {
		StatusStr string `json:"status_str"`
		Completed bool   `json:"completed"`
	} `json:"status"`
	Outputs map[string]struct {
		Images []OutputImage `json:"images"`
	} `json:"outputs"`
}

// History fetches the history entry for a prompt, or nil if not finished.
func (c *Client) History(ctx context.Context, promptID string) (*HistoryEntry, error) {
	var h map[string]HistoryEntry
	if err := c.getJSON(ctx, "/history/"+url.PathEscape(promptID), &h); err != nil {
		return nil, err
	}
	entry, ok := h[promptID]
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

// Wait polls history until the prompt completes or ctx is done.
func (c *Client) Wait(ctx context.Context, promptID string, interval time.Duration, onPhase func(phase string)) (*HistoryEntry, error) {
	if interval <= 0 {
		interval = time.Duration(c.cfg.PollIntervalMs) * time.Millisecond
	}
	if interval <= 0 {
		interval = time.Second
	}
	if onPhase != nil {
		onPhase("generate")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		entry, err := c.History(ctx, promptID)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			return entry, nil
		}
	}
}

// Fetch downloads an output image.
func (c *Client) Fetch(ctx context.Context, img OutputImage) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("filename", img.Filename)
	q.Set("subfolder", img.Subfolder)
	q.Set("type", img.Type)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/view?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comfyui: fetch output: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("comfyui: fetch output: %s", resp.Status)
	}
	return resp.Body, nil
}

// Interrupt requests cancellation of the current execution.
func (c *Client) Interrupt(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/interrupt", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) base() string { return strings.TrimRight(c.cfg.BaseURL, "/") }

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("comfyui: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("comfyui: GET %s: %s: %s", path, resp.Status, string(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("comfyui: decode %s: %w", path, err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
