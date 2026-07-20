package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultPreset = "calm"

type Config struct {
	Preset string `json:"preset"`
}

func ValidPreset(value string) bool {
	switch value {
	case "calm", "vivid", "max":
		return true
	default:
		return false
	}
}

func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Enterlight", "config.json"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{Preset: DefaultPreset}, err
	}
	return LoadPath(path)
}

func LoadPath(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Preset: DefaultPreset}, nil
	}
	if err != nil {
		return Config{Preset: DefaultPreset}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{Preset: DefaultPreset}, fmt.Errorf("parse %s: %w", path, err)
	}
	if !ValidPreset(cfg.Preset) {
		return Config{Preset: DefaultPreset}, fmt.Errorf("parse %s: unknown preset %q", path, cfg.Preset)
	}
	return cfg, nil
}

func Save(cfg Config) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	return path, SavePath(path, cfg)
}

func SavePath(path string, cfg Config) error {
	if !ValidPreset(cfg.Preset) {
		return fmt.Errorf("unknown preset %q", cfg.Preset)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
