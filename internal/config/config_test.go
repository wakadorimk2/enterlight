package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPathDefaultsWhenMissing(t *testing.T) {
	cfg, err := LoadPath(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Preset != DefaultPreset {
		t.Fatalf("preset = %q, want %q", cfg.Preset, DefaultPreset)
	}
}

func TestSaveAndLoadPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := SavePath(path, Config{Preset: "vivid"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Preset != "vivid" {
		t.Fatalf("preset = %q, want vivid", cfg.Preset)
	}
}

func TestLoadPathFallsBackOnInvalidPreset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"preset":"loud"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadPath(path)
	if err == nil {
		t.Fatal("expected invalid preset error")
	}
	if cfg.Preset != DefaultPreset {
		t.Fatalf("preset = %q, want fallback %q", cfg.Preset, DefaultPreset)
	}
}
