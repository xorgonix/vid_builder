package testutil

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// LMStudio is a fake OpenAI-compatible LM Studio server.
type LMStudio struct {
	*httptest.Server
	mu     sync.Mutex
	reply  string
	models []string
	calls  int
}

// NewLMStudio starts a fake LM Studio server. Default reply is a short sentence.
func NewLMStudio(t *testing.T) *LMStudio {
	t.Helper()
	f := &LMStudio{reply: "Planner reply.", models: []string{"test-model"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		data := make([]map[string]any, 0, len(f.models))
		for _, m := range f.models {
			data = append(data, map[string]any{"id": m, "object": "model", "state": "loaded", "type": "llm"})
		}
		writeJSON(w, map[string]any{"data": data})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls++
		reply := f.reply
		f.mu.Unlock()
		writeJSON(w, map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": reply}, "finish_reason": "stop"},
			},
		})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	return f
}

// SetReply sets the assistant content returned by every completion.
func (f *LMStudio) SetReply(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply = text
}

// SetJSON sets the assistant content to a JSON encoding of v.
func (f *LMStudio) SetJSON(v any) {
	b, _ := json.Marshal(v)
	f.SetReply(string(b))
}

// Calls reports how many completions were requested.
func (f *LMStudio) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ComfyUI is a fake ComfyUI server covering the calls the adapter makes.
type ComfyUI struct {
	*httptest.Server
	mu             sync.Mutex
	checkpoints    []string
	fail           bool
	promptID       string
	uploads        int
	lastPrompt     map[string]any
	outputFilename string
}

// NewComfyUI starts a fake ComfyUI server.
func NewComfyUI(t *testing.T) *ComfyUI {
	t.Helper()
	f := &ComfyUI{checkpoints: []string{"photon_v1.safetensors"}, promptID: "fake-prompt-1", outputFilename: "fake_00001_.png"}
	mux := http.NewServeMux()

	mux.HandleFunc("/system_stats", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		fail := f.fail
		f.mu.Unlock()
		if fail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"system":  map[string]any{"comfyui_version": "0.0-test", "python_version": "3.12"},
			"devices": []map[string]any{{"name": "fake-gpu", "type": "cuda", "vram_total": int64(25769803776), "vram_free": int64(24000000000)}},
		})
	})

	mux.HandleFunc("/object_info/", func(w http.ResponseWriter, r *http.Request) {
		class := strings.TrimPrefix(r.URL.Path, "/object_info/")
		if class == "CheckpointLoaderSimple" {
			f.mu.Lock()
			cps := append([]string(nil), f.checkpoints...)
			f.mu.Unlock()
			writeJSON(w, map[string]any{
				"CheckpointLoaderSimple": map[string]any{
					"input": map[string]any{"required": map[string]any{"ckpt_name": []any{cps}}},
				},
			})
			return
		}
		// Any other class exists with no required inputs.
		writeJSON(w, map[string]any{class: map[string]any{"input": map[string]any{"required": map[string]any{}}}})
	})
	mux.HandleFunc("/object_info", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{}) })

	mux.HandleFunc("/prompt", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt map[string]any `json:"prompt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.lastPrompt = body.Prompt
		id := f.promptID
		f.mu.Unlock()
		writeJSON(w, map[string]any{"prompt_id": id, "number": 1, "node_errors": map[string]any{}})
	})

	mux.HandleFunc("/history/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		id := f.promptID
		filename := f.outputFilename
		f.mu.Unlock()
		writeJSON(w, map[string]any{
			id: map[string]any{
				"status": map[string]any{"status_str": "success", "completed": true},
				"outputs": map[string]any{
					"7":  map[string]any{"images": []map[string]any{{"filename": filename, "subfolder": "", "type": "output"}}},
					"12": map[string]any{"images": []map[string]any{{"filename": filename, "subfolder": "", "type": "output"}}},
				},
			},
		})
	})

	mux.HandleFunc("/view", func(w http.ResponseWriter, r *http.Request) {
		png, _ := base64.StdEncoding.DecodeString(tinyPNGBase64)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})

	mux.HandleFunc("/upload/image", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.uploads++
		n := f.uploads
		f.mu.Unlock()
		writeJSON(w, map[string]any{"name": fmt.Sprintf("uploaded_%d.png", n), "subfolder": "", "type": "input"})
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	return f
}

// SetCheckpoints replaces the reported checkpoint list.
func (f *ComfyUI) SetCheckpoints(list ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkpoints = list
}

// SetDown makes /system_stats fail, simulating an unreachable server.
func (f *ComfyUI) SetDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = down
}

// Uploads reports how many images were staged.
func (f *ComfyUI) Uploads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploads
}

// LastPrompt returns the most recently submitted graph (API format), so tests can
// assert which bindings actually reached the workflow.
func (f *ComfyUI) LastPrompt() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPrompt
}

// SetOutputFilename controls the filename the fake reports for outputs.
func (f *ComfyUI) SetOutputFilename(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outputFilename = name
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// A valid 1x1 PNG so thumbnail generation succeeds in tests.
const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
