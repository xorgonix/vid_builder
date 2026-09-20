// Package config loads runtime configuration from config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the complete runtime configuration. All runtime variables live in
// config.json (which is gitignored) — never in code.
type Config struct {
	Server    ServerConfig    `json:"server"`
	Database  DatabaseConfig  `json:"database"`
	Media     MediaConfig     `json:"media"`
	LLM       LLMConfig       `json:"llm"`
	ComfyUI   ComfyUIConfig   `json:"comfyui"`
	FFmpeg    FFmpegConfig    `json:"ffmpeg"`
	Auth      AuthConfig      `json:"auth"`
	Security  SecurityConfig  `json:"security"`
	Worker    WorkerConfig    `json:"worker"`
	Logging   LoggingConfig   `json:"logging"`
	Bootstrap BootstrapConfig `json:"bootstrap"`
}

// BootstrapConfig defines the first-run administrator account created when the
// database has no users yet. It lives in config.json and is never committed.
type BootstrapConfig struct {
	AdminEmail    string `json:"admin_email"`
	AdminPassword string `json:"admin_password"`
	AdminName     string `json:"admin_name"`
}

type ServerConfig struct {
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	Mode                 string `json:"mode"` // desktop | local-web | lan | server
	BootstrapTokenTTLMin int    `json:"bootstrap_token_ttl_minutes"`
	LANBindHost          string `json:"lan_bind_host"`
	LANPort              int    `json:"lan_port"`
}

type DatabaseConfig struct {
	Path          string `json:"path"`
	BusyTimeoutMs int    `json:"busy_timeout_ms"`
	MaxPoolConns  int    `json:"max_pool_conns"`
}

type MediaConfig struct {
	RootDir      string `json:"root_dir"`
	TempDir      string `json:"temp_dir"`
	ThumbnailDir string `json:"thumbnail_dir"`
	ExportsDir   string `json:"exports_dir"`
}

type LLMConfig struct {
	Enabled             bool    `json:"enabled"`
	BaseURL             string  `json:"base_url"`
	Model               string  `json:"model"`
	APIKey              string  `json:"api_key"`
	Temperature         float64 `json:"temperature"`
	MaxTokens           int     `json:"max_tokens"`
	RequestTimeoutSec   int     `json:"request_timeout_seconds"`
	PlannerSystemPrompt string  `json:"planner_system_prompt"`
	UnloadBeforeGPU     bool    `json:"unload_before_gpu"`
	UnloadCommand       string  `json:"unload_command"`
	ReloadCommand       string  `json:"reload_command"`
}

type ComfyUIConfig struct {
	Enabled           bool   `json:"enabled"`
	BaseURL           string `json:"base_url"`
	RequestTimeoutSec int    `json:"request_timeout_seconds"`
	PollIntervalMs    int    `json:"poll_interval_ms"`
}

type FFmpegConfig struct {
	Enabled           bool   `json:"enabled"`
	BinaryPath        string `json:"binary_path"`
	RequestTimeoutSec int    `json:"request_timeout_seconds"`
}

type AuthConfig struct {
	SessionTTLHours   int `json:"session_ttl_hours"`
	Argon2MemoryKB    int `json:"argon2_memory_kb"`
	Argon2Iterations  int `json:"argon2_iterations"`
	Argon2Parallelism int `json:"argon2_parallelism"`
}

type SecurityConfig struct {
	CookieSecure       bool   `json:"cookie_secure"`
	CookieName         string `json:"cookie_name"`
	RateLimitPerMinute int    `json:"rate_limit_per_minute"`
}

type WorkerConfig struct {
	PollIntervalMs  int `json:"poll_interval_ms"`
	LeaseTTLSeconds int `json:"lease_ttl_seconds"`
	MaxAttempts     int `json:"max_attempts"`
}

type LoggingConfig struct {
	Level   string `json:"level"`
	File    string `json:"file"`
	Console bool   `json:"console"`
}

// Default returns a sensible default configuration matching the shipped
// config.json so a missing file can be recreated or validated.
func Default() *Config {
	return &Config{
		Server:   ServerConfig{Host: "127.0.0.1", Port: 8971, Mode: "local-web", BootstrapTokenTTLMin: 10, LANBindHost: "0.0.0.0", LANPort: 8972},
		Database: DatabaseConfig{Path: "data/vidbuild.db", BusyTimeoutMs: 5000, MaxPoolConns: 8},
		Media:    MediaConfig{RootDir: "data/media", TempDir: "data/temp", ThumbnailDir: "data/media/thumbs", ExportsDir: "data/exports"},
		LLM: LLMConfig{
			Enabled: true, BaseURL: "http://127.0.0.1:1234",
			Model:       "qwen3.5-9b-uncensored-hauhaucs-aggressive",
			Temperature: 0.7, MaxTokens: 8192, RequestTimeoutSec: 180,
			PlannerSystemPrompt: "You are vid-build's planner, a video pre-production assistant. You help creators develop briefs, outlines, shot lists, prompts and blocking. You propose; you never decide. Always respond with the requested structured JSON and nothing else.",
			UnloadCommand:       "lms unload --all",
		},
		ComfyUI:  ComfyUIConfig{Enabled: false, BaseURL: "http://127.0.0.1:8188", RequestTimeoutSec: 30, PollIntervalMs: 1000},
		FFmpeg:   FFmpegConfig{Enabled: false, BinaryPath: "ffmpeg", RequestTimeoutSec: 600},
		Auth:     AuthConfig{SessionTTLHours: 720, Argon2MemoryKB: 65536, Argon2Iterations: 2, Argon2Parallelism: 2},
		Security: SecurityConfig{CookieSecure: false, CookieName: "vidbuild_session", RateLimitPerMinute: 20},
		Worker:   WorkerConfig{PollIntervalMs: 1500, LeaseTTLSeconds: 120, MaxAttempts: 3},
		Logging:  LoggingConfig{Level: "info", File: "data/logs/vidbuild.log", Console: true},
		Bootstrap: BootstrapConfig{
			AdminEmail: "admin@vidbuild.local", AdminPassword: "vidbuild", AdminName: "Studio Admin",
		},
	}
}

// Load reads config.json from path, overlays it on defaults and validates.
func Load(path string) (*Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadOrCreate loads config.json, writing defaults first if it does not exist.
func LoadOrCreate(path string) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		b, _ := json.MarshalIndent(cfg, "", "  ")
		if werr := os.WriteFile(path, b, 0o600); werr != nil {
			return nil, fmt.Errorf("write default config %s: %w", path, werr)
		}
		return cfg, cfg.Validate()
	}
	return Load(path)
}

// Validate checks required values after defaults are applied.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("config: server.port must be 1-65535, got %d", c.Server.Port)
	}
	switch c.Server.Mode {
	case "desktop", "local-web", "lan", "server":
	default:
		return fmt.Errorf("config: server.mode must be desktop|local-web|lan|server, got %q", c.Server.Mode)
	}
	if c.Database.Path == "" {
		return fmt.Errorf("config: database.path is required")
	}
	if c.LLM.Enabled {
		if c.LLM.BaseURL == "" {
			return fmt.Errorf("config: llm.base_url is required when llm.enabled")
		}
		if c.LLM.Model == "" {
			return fmt.Errorf("config: llm.model is required when llm.enabled")
		}
	}
	return nil
}

// EnsureDirs creates all configured directories relative to basePath.
func (c *Config) EnsureDirs(basePath string) error {
	dirs := []string{
		c.Database.Path,
		c.Media.RootDir, c.Media.TempDir, c.Media.ThumbnailDir, c.Media.ExportsDir,
		c.Logging.File,
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		full := d
		if !filepath.IsAbs(d) {
			full = filepath.Join(basePath, d)
		}
		if dir := filepath.Dir(full); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create dir %s: %w", dir, err)
			}
		}
	}
	return nil
}

// AbsPath resolves a possibly-relative configured path against basePath.
func (c *Config) AbsPath(basePath, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(basePath, p)
}
