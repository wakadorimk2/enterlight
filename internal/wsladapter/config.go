package wsladapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const SupportedCodexVersion = "0.144.6"

type Config struct {
	CodexPath     string `json:"codexPath"`
	CodexVersion  string `json:"codexVersion"`
	WindowsBinary string `json:"windowsBinary"`
}

func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "enterlight", "wsl.json"), nil
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.CodexPath == "" || cfg.WindowsBinary == "" {
		return cfg, errors.New("config is missing a required executable path")
	}
	if !filepath.IsAbs(cfg.CodexPath) || !filepath.IsAbs(cfg.WindowsBinary) {
		return cfg, errors.New("configured executable paths must be absolute")
	}
	if cfg.CodexVersion != SupportedCodexVersion {
		return cfg, fmt.Errorf("unsupported Codex CLI version %q (expected %s)", cfg.CodexVersion, SupportedCodexVersion)
	}
	return cfg, nil
}

func ValidateExecutables(cfg Config, helperPath string) error {
	realInfo, err := os.Stat(cfg.CodexPath)
	if err != nil {
		return fmt.Errorf("Codex executable: %w", err)
	}
	if realInfo.IsDir() || realInfo.Mode()&0o111 == 0 {
		return errors.New("configured Codex path is not executable")
	}
	helperInfo, err := os.Stat(helperPath)
	if err == nil && os.SameFile(realInfo, helperInfo) {
		return errors.New("refusing recursive Codex adapter configuration")
	}
	realResolved, realErr := filepath.EvalSymlinks(cfg.CodexPath)
	helperResolved, helperErr := filepath.EvalSymlinks(helperPath)
	if realErr == nil && helperErr == nil && realResolved == helperResolved {
		return errors.New("refusing recursive Codex adapter configuration")
	}
	winInfo, err := os.Stat(cfg.WindowsBinary)
	if err != nil {
		return fmt.Errorf("Windows enterlight executable: %w", err)
	}
	if winInfo.IsDir() {
		return errors.New("configured Windows enterlight path is a directory")
	}
	return nil
}
