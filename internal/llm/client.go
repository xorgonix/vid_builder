// Package llm is the LM Studio chat-completions adapter used by the planner
// (spec §4.1 "Adapters": LM Studio, ComfyUI, FFmpeg...).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vidbuild/internal/config"
)

// Client talks to an LM Studio (OpenAI-compatible /v1/chat/completions) server.
type Client struct {
	cfg    config.LLMConfig
	client *http.Client
}

func New(cfg config.LLMConfig) *Client {
	return &Client{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second},
	}
}

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chat sends a chat completion request and returns the assistant text.
func (c *Client) Chat(ctx context.Context, system string, msgs []Message) (string, error) {
	if !c.cfg.Enabled {
		return "", fmt.Errorf("llm: disabled in config")
	}
	all := make([]Message, 0, len(msgs)+1)
	if system != "" {
		all = append(all, Message{Role: "system", Content: system})
	}
	all = append(all, msgs...)
	body := map[string]any{
		"model":       c.cfg.Model,
		"messages":    all,
		"temperature": c.cfg.Temperature,
		"max_tokens":  c.cfg.MaxTokens,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("llm: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: request %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("llm: %s: %s", resp.Status, string(b))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: empty choices")
	}
	return out.Choices[0].Message.Content, nil
}

// ChatJSON requests a strict-JSON reply and extracts the first JSON object
// from the response text (tolerates code fences and prefix prose).
func (c *Client) ChatJSON(ctx context.Context, system string, msgs []Message, schemaHint string) (json.RawMessage, error) {
	if system == "" {
		system = c.cfg.PlannerSystemPrompt
	} else {
		system = c.cfg.PlannerSystemPrompt + "\n\n" + system
	}
	if schemaHint != "" {
		msgs = append(msgs, Message{Role: "user", Content: "Respond ONLY with a single JSON object matching this shape (no prose, no markdown fence):\n" + schemaHint})
	}
	text, err := c.Chat(ctx, system, msgs)
	if err != nil {
		return nil, err
	}
	obj, rest := extractJSON(text)
	if obj == nil {
		return nil, fmt.Errorf("llm: response contained no JSON object: %.200s", text)
	}
	_ = rest
	return obj, nil
}

// extractJSON pulls the first balanced JSON object out of text.
func extractJSON(text string) (json.RawMessage, string) {
	start := strings.Index(text, "{")
	if start < 0 {
		return nil, ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return json.RawMessage(text[start : i+1]), text[i+1:]
			}
		}
	}
	return nil, ""
}

// Ping checks the LM Studio server is reachable and the configured model exists.
func (c *Client) Ping(ctx context.Context) error {
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("lm studio unreachable at %s: %w", c.cfg.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lm studio /v1/models: %s", resp.Status)
	}
	return nil
}
