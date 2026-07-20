package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type hookFile map[string]any

func Install(executable string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".codex")
	path := filepath.Join(dir, "hooks.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	root := hookFile{}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) > 0 {
			if err := json.Unmarshal(data, &root); err != nil {
				return "", fmt.Errorf("parse %s: %w", path, err)
			}
		}
		_ = os.WriteFile(path+".bak", data, 0o644)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	removeEnterlightHooks(root)
	hooks := ensureMap(root, "hooks")
	commandBase := quoteCommand(executable) + " codex-hook "
	appendHook(hooks, "UserPromptSubmit", commandBase+"working", "Enterlight: working")
	appendHook(hooks, "PreToolUse", commandBase+"working", "Enterlight: working")
	appendHook(hooks, "PermissionRequest", commandBase+"waiting", "Enterlight: waiting")
	appendHook(hooks, "Stop", commandBase+"done", "Enterlight: done")

	root["description"] = "Local lifecycle hooks, including Enterlight agent status lighting."
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func Uninstall() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".codex", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	root := hookFile{}
	if err := json.Unmarshal(data, &root); err != nil {
		return "", err
	}
	removeEnterlightHooks(root)
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	out = append(out, '\n')
	return path, os.WriteFile(path, out, 0o644)
}

func removeEnterlightHooks(root hookFile) {
	raw, ok := root["hooks"].(map[string]any)
	if !ok {
		return
	}
	for event, value := range raw {
		groups, ok := value.([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(groups))
		for _, group := range groups {
			if !containsMarker(group) {
				kept = append(kept, group)
			}
		}
		if len(kept) == 0 {
			delete(raw, event)
		} else {
			raw[event] = kept
		}
	}
}

func containsMarker(value any) bool {
	data, _ := json.Marshal(value)
	text := strings.ToLower(string(data))
	return strings.Contains(text, "enterlight") && strings.Contains(text, "codex-hook")
}

func ensureMap(root map[string]any, key string) map[string]any {
	if existing, ok := root[key].(map[string]any); ok {
		return existing
	}
	value := map[string]any{}
	root[key] = value
	return value
}

func appendHook(hooks map[string]any, event, command, status string) {
	handler := map[string]any{
		"type":          "command",
		"command":       command,
		"timeout":       5,
		"statusMessage": status,
	}
	if runtime.GOOS == "windows" {
		handler["commandWindows"] = command
	}
	group := map[string]any{"hooks": []any{handler}}
	existing, _ := hooks[event].([]any)
	hooks[event] = append(existing, group)
}

func quoteCommand(path string) string {
	if strings.ContainsAny(path, " \t\"") {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return path
}
