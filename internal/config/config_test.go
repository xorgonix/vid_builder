package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultValidates(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestLoadOrCreateWritesThenLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port == 0 || cfg.LLM.Model == "" {
		t.Fatal("defaults not populated")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	again, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Security.CookieName != cfg.Security.CookieName {
		t.Fatal("reload mismatch")
	}
}

func TestInvalidModeRejected(t *testing.T) {
	cfg := Default()
	cfg.Server.Mode = "nonsense"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for bad mode")
	}
}
