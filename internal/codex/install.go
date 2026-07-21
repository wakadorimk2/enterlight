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
	path := filepath.Join(home, ".codex", "hooks.json")
	commandBase := quoteCommand(executable) + " codex-hook "
	commandWindowsBase := ""
	if runtime.GOOS == "windows" {
		commandWindowsBase = commandBase
	}
	if err := installAt(path, commandBase, commandWindowsBase, true); err != nil {
		return "", err
	}
	return path, nil
}

// InstallAt replaces only Enterlight lifecycle handlers in a Codex hooks file.
// commandBase and commandWindowsBase must include the trailing space before the
// lifecycle state.
func InstallAt(path, commandBase, commandWindowsBase string) error {
	return installAt(path, commandBase, commandWindowsBase, false)
}

func installAt(path, commandBase, commandWindowsBase string, setDescription bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	root := hookFile{}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) > 0 {
			if err := json.Unmarshal(data, &root); err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
		}
		if err := os.WriteFile(path+".bak", data, 0o644); err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	removeEnterlightHooks(root)
	hooks := ensureMap(root, "hooks")
	appendHook(hooks, "UserPromptSubmit", commandBase+"working", commandWithState(commandWindowsBase, "working"), "Enterlight: working")
	appendHook(hooks, "PreToolUse", commandBase+"working", commandWithState(commandWindowsBase, "working"), "Enterlight: working")
	appendHook(hooks, "PermissionRequest", commandBase+"waiting", commandWithState(commandWindowsBase, "waiting"), "Enterlight: waiting")
	appendHook(hooks, "Stop", commandBase+"done", commandWithState(commandWindowsBase, "done"), "Enterlight: done")

	if setDescription {
		root["description"] = "Local lifecycle hooks, including Enterlight agent status lighting."
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return nil
}

func Uninstall() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".codex", "hooks.json")
	return path, UninstallAt(path)
}

// UninstallAt removes only Enterlight lifecycle handlers from a Codex hooks file.
func UninstallAt(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	root := hookFile{}
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	removeEnterlightHooks(root)
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
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

func appendHook(hooks map[string]any, event, command, commandWindows, status string) {
	handler := map[string]any{
		"type":          "command",
		"command":       command,
		"timeout":       5,
		"statusMessage": status,
	}
	if commandWindows != "" {
		handler["commandWindows"] = commandWindows
	}
	group := map[string]any{"hooks": []any{handler}}
	existing, _ := hooks[event].([]any)
	hooks[event] = append(existing, group)
}

func commandWithState(base, state string) string {
	if base == "" {
		return ""
	}
	return base + state
}

func quoteCommand(path string) string {
	if strings.ContainsAny(path, " \t\"") {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return path
}
