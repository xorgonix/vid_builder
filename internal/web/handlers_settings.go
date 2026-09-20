package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"vidbuild/internal/comfyui"
	"vidbuild/internal/templates"
)

// Capabilities is the host capability document (spec §4.2).
type Capabilities struct {
	Desktop           bool `json:"desktop"`
	NativeFileDialogs bool `json:"native_file_dialogs"`
	RevealFile        bool `json:"reveal_file"`
	LocalGPU          bool `json:"local_gpu"`
	LANAccess         bool `json:"lan_access"`
	ComfyUIEnabled    bool `json:"comfyui_enabled"`
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	caps := Capabilities{
		Desktop:        false,
		LocalGPU:       s.cfg.ComfyUI.Enabled,
		LANAccess:      s.cfg.Server.Mode == "lan",
		ComfyUIEnabled: s.cfg.ComfyUI.Enabled,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(caps)
}

// LocalServicesData backs the local-services settings page.
func localServicesData(s *Server, r *http.Request) templates.LocalServicesData {
	data := templates.LocalServicesData{
		Shell:       s.shell(r, "Local services"),
		LLMModel:    s.cfg.LLM.Model,
		LLMBase:     s.cfg.LLM.BaseURL,
		ComfyBase:   s.cfg.ComfyUI.BaseURL,
		FFmpegBin:   s.cfg.FFmpeg.BinaryPath,
		LLMEnabled:  s.cfg.LLM.Enabled,
		ComfyEnable: s.cfg.ComfyUI.Enabled,
	}
	return data
}

func (s *Server) handleLocalServices(w http.ResponseWriter, r *http.Request) {
	data := localServicesData(s, r)
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	s.renderPage(w, r, templates.LocalServicesPage(data))
}

func (s *Server) handleTestLocalServices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	results := map[string]string{}

	// LM Studio
	if s.cfg.LLM.Enabled {
		if err := s.planner.Health(ctx); err != nil {
			results["llm"] = "error: " + err.Error()
		} else {
			results["llm"] = "ok (" + s.cfg.LLM.Model + ")"
		}
	} else {
		results["llm"] = "disabled"
	}

	// ComfyUI
	client := comfyui.New(s.cfg.ComfyUI)
	if stats, err := client.Stats(ctx); err != nil {
		results["comfyui"] = "error: " + err.Error()
	} else {
		vram := int64(0)
		if len(stats.Devices) > 0 {
			vram = stats.Devices[0].VRAMTotal
		}
		results["comfyui"] = "ok (v" + stats.System.ComfyUIVersion + ", VRAM " + itoa(int(vram/(1<<30))) + "GB)"
	}

	// FFmpeg
	if _, err := exec.LookPath(s.cfg.FFmpeg.BinaryPath); err != nil {
		results["ffmpeg"] = "error: " + err.Error()
	} else {
		results["ffmpeg"] = "ok"
	}

	msg := "LM Studio: " + results["llm"] + " · ComfyUI: " + results["comfyui"] + " · FFmpeg: " + results["ffmpeg"]
	kind := "success"
	if containsAny(results["llm"], "error") || containsAny(results["comfyui"], "error") {
		kind = "warning"
	}
	http.Redirect(w, r, withQuery("/settings/local-services", flashQuery(msg, kind)), http.StatusSeeOther)
}

func containsAny(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "Diagnostics")
	shell.ActiveNav = "settings"
	d := templates.DiagnosticsData{Shell: shell}
	ctx := r.Context()

	_ = s.store.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE state='queued'`).Scan(&d.JobsQueued)
	_ = s.store.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE state IN ('claimed','running')`).Scan(&d.JobsRunning)
	_ = s.store.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE state='failed'`).Scan(&d.JobsFailed)
	_ = s.store.QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_observations WHERE success=0`).Scan(&d.ObservationsFailed)
	_ = s.store.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets WHERE deleted_at IS NULL`).Scan(&d.AssetCount)
	_ = s.store.QueryRowContext(ctx, `SELECT COUNT(*) FROM generation_attempts`).Scan(&d.AttemptCount)

	var integrity string
	_ = s.store.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity)
	d.DBIntegrity = integrity
	var fk int
	_ = s.store.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fk)
	d.FKViolations = fk
	var jm string
	_ = s.store.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&jm)
	d.JournalMode = jm

	if st, err := os.Stat(s.cfg.Media.RootDir); err == nil && st.IsDir() {
		d.MediaRootOK = true
	}
	if wd, err := os.Getwd(); err == nil {
		d.WorkingDir = wd
	}
	d.Platform = runtime.GOOS + "/" + runtime.GOARCH
	_ = ws

	s.renderPage(w, r, templates.DiagnosticsPage(d))
}
