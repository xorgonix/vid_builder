package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"vidbuild/internal/backup"
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
		LANAccess:      s.lan.enabled(),
		ComfyUIEnabled: s.cfg.ComfyUI.Enabled,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(caps)
}

// LocalServicesData backs the local-services settings page.
func localServicesData(s *Server, r *http.Request) templates.LocalServicesData {
	data := templates.LocalServicesData{
		Shell:           s.shell(r, "Local services"),
		LLMModel:        s.cfg.LLM.Model,
		LLMBase:         s.cfg.LLM.BaseURL,
		ComfyBase:       s.cfg.ComfyUI.BaseURL,
		FFmpegBin:       s.cfg.FFmpeg.BinaryPath,
		LLMEnabled:      s.cfg.LLM.Enabled,
		ComfyEnable:     s.cfg.ComfyUI.Enabled,
		UnloadBeforeGPU: s.cfg.LLM.UnloadBeforeGPU,
		UnloadCommand:   s.cfg.LLM.UnloadCommand,
	}
	return data
}

// handleUnloadPlanner runs the configured unload command on demand.
func (s *Server) handleUnloadPlanner(w http.ResponseWriter, r *http.Request) {
	cmdline := strings.TrimSpace(s.cfg.LLM.UnloadCommand)
	if cmdline == "" {
		http.Redirect(w, r, "/settings/local-services?msg="+urlMsg("No unload command configured.")+"&kind=error", http.StatusSeeOther)
		return
	}
	fields := strings.Fields(cmdline)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, fields[0], fields[1:]...).CombinedOutput()
	if err != nil {
		s.log.Warn("unload planner", "err", err)
		http.Redirect(w, r, "/settings/local-services?msg="+urlMsg("Unload failed: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = out
	http.Redirect(w, r, "/settings/local-services?msg="+urlMsg("Planner memory freed.")+"&kind=success", http.StatusSeeOther)
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
	d.IsOwner = RoleFrom(r.Context()) == "owner"
	if list, err := backup.List(s.backupsDir()); err == nil {
		for _, e := range list {
			d.Backups = append(d.Backups, templates.BackupEntryView{Name: e.Name, CreatedAt: e.CreatedAt, SizeBytes: e.SizeBytes, Complete: e.Complete, Media: e.Media})
		}
	}
	_ = ws

	s.renderPage(w, r, templates.DiagnosticsPage(d))
}

// backupsDir returns the configured backup directory (sibling of the database).
func (s *Server) backupsDir() string {
	dir := filepath.Join(filepath.Dir(s.cfg.Database.Path), "backups")
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	job, err := s.store.EmitBackupJob(r.Context(), ws.ID)
	if err != nil {
		s.log.Error("emit backup job", "err", err)
		http.Redirect(w, r, "/w/"+ws.ID+"/diagnostics?msg="+urlMsg("Could not queue backup.")+"&kind=error", http.StatusSeeOther)
		return
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "backup.create", "database", "all", "queued backup", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/diagnostics?msg="+urlMsg("Backup queued ("+job.ID[:8]+"). Refresh when the worker finishes.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleVerifyBackup(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	_ = r.ParseForm()
	name := filepath.Base(r.FormValue("name"))
	if name == "" || name == "." || name == ".." {
		http.Redirect(w, r, "/w/"+ws.ID+"/diagnostics?msg="+urlMsg("No backup selected.")+"&kind=error", http.StatusSeeOther)
		return
	}
	res, err := backup.Verify(r.Context(), filepath.Join(s.backupsDir(), name), s.cfg.Media.RootDir)
	if err != nil {
		http.Redirect(w, r, "/w/"+ws.ID+"/diagnostics?msg="+urlMsg("Verify failed: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	if !res.DBOK {
		http.Redirect(w, r, "/w/"+ws.ID+"/diagnostics?msg="+urlMsg("Backup database failed integrity: "+res.DBError)+"&kind=error", http.StatusSeeOther)
		return
	}
	kind := "success"
	msg := "Backup OK: database integrity ok, " + itoa(res.MediaOK) + "/" + itoa(res.MediaTotal) + " media checksums match."
	if len(res.MediaBad) > 0 {
		kind = "warning"
		msg = "Backup database OK, but " + itoa(len(res.MediaBad)) + " media file(s) missing or changed: " + res.MediaBad[0]
	}
	http.Redirect(w, r, "/w/"+ws.ID+"/diagnostics?msg="+urlMsg(msg)+"&kind="+kind, http.StatusSeeOther)
}
