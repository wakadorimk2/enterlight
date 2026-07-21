//go:build linux

package wsladapter

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallAndUninstallScripts(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	fakeBin := filepath.Join(root, "fake-bin")
	localBin := filepath.Join(home, ".local", "bin")
	for _, dir := range []string{home, fakeBin, localBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeExecutable(t, filepath.Join(fakeBin, "uname"), "#!/bin/sh\necho 5.15.0-microsoft-standard-WSL2\n")
	codexPath := filepath.Join(fakeBin, "codex")
	writeExecutable(t, codexPath, "#!/bin/sh\necho codex-cli "+SupportedCodexVersion+"\n")
	helperPath := filepath.Join(root, "enterlight-linux-amd64")
	writeExecutable(t, helperPath, "#!/bin/sh\nexit 0\n")
	windowsPath := filepath.Join(root, "enterlight.exe")
	writeExecutable(t, windowsPath, "#!/bin/sh\nexit 0\n")

	_, testFile, _, _ := runtime.Caller(0)
	installScript := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", "..", "scripts", "install-wsl.sh"))
	uninstallScript := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", "..", "scripts", "uninstall-wsl.sh"))
	env := append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME=", "WSL_INTEROP=/run/WSL/1_interop", "PATH="+localBin+":"+fakeBin+":"+os.Getenv("PATH"))

	cmd := exec.Command("bash", installScript, "--helper", helperPath, "--windows-exe", windowsPath)
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}

	shim := filepath.Join(localBin, "codex")
	shimData, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shimData), "Managed by Enterlight") {
		t.Fatalf("unmanaged shim: %s", shimData)
	}
	configData, err := os.ReadFile(filepath.Join(home, ".config", "enterlight", "wsl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(configData, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CodexPath != codexPath || cfg.WindowsBinary != windowsPath || cfg.CodexVersion != SupportedCodexVersion || cfg.CodexHooksManaged {
		t.Fatalf("config = %#v", cfg)
	}

	cmd = exec.Command("bash", uninstallScript)
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, output)
	}
	for _, path := range []string{shim, filepath.Join(home, ".local", "lib", "enterlight", "enterlight-linux-amd64"), filepath.Join(home, ".config", "enterlight", "wsl.json")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("managed path remains: %s", path)
		}
	}

	if err := os.WriteFile(shim, []byte("#!/bin/sh\necho keep-me\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("bash", installScript, "--helper", helperPath, "--windows-exe", windowsPath)
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "Refusing to overwrite") {
		t.Fatalf("existing shim was not protected: err=%v output=%s", err, output)
	}
	data, _ := os.ReadFile(shim)
	if !strings.Contains(string(data), "keep-me") {
		t.Fatal("existing shim was modified")
	}

	if err := os.Remove(shim); err != nil {
		t.Fatal(err)
	}
	wrongOrderEnv := append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME=", "WSL_INTEROP=/run/WSL/1_interop", "PATH="+fakeBin+":"+localBin+":"+os.Getenv("PATH"))
	cmd = exec.Command("bash", installScript, "--helper", helperPath, "--windows-exe", windowsPath)
	cmd.Env = wrongOrderEnv
	if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "managed shim would not run") {
		t.Fatalf("PATH precedence was not rejected: err=%v output=%s", err, output)
	}
}

func TestInstallAndUninstallManagedCodexHooks(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	fakeBin := filepath.Join(root, "fake-bin")
	localBin := filepath.Join(home, ".local", "bin")
	for _, dir := range []string{home, fakeBin, localBin, filepath.Join(home, ".codex")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeExecutable(t, filepath.Join(fakeBin, "uname"), "#!/bin/sh\necho 5.15.0-microsoft-standard-WSL2\n")
	codexPath := filepath.Join(fakeBin, "codex")
	writeExecutable(t, codexPath, "#!/bin/sh\necho codex-cli "+SupportedCodexVersion+"\n")
	writeExecutable(t, filepath.Join(fakeBin, "wslpath"), "#!/bin/sh\nprintf '%s\\n' 'C:\\Program Files\\Enterlight\\enterlight.exe'\n")
	writeExecutable(t, filepath.Join(fakeBin, "powershell.exe"), "#!/bin/sh\necho noisy output\necho noisy error >&2\nif [ \"${POWERSHELL_DELAY:-0}\" = 1 ]; then sleep 10; fi\nexit \"${POWERSHELL_EXIT_CODE:-0}\"\n")
	windowsPath := filepath.Join(root, "enterlight.exe")
	writeExecutable(t, windowsPath, "#!/bin/sh\nexit 0\n")

	_, testFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))
	installScript := filepath.Join(repoRoot, "scripts", "install-wsl.sh")
	uninstallScript := filepath.Join(repoRoot, "scripts", "uninstall-wsl.sh")
	helperPath := filepath.Join(root, "enterlight-linux-amd64")
	build := exec.Command("go", "build", "-o", helperPath, "./cmd/enterlight-wsl")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, "go-cache"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}

	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	originalHooks := []byte(`{
  "custom": "keep-me",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Read", "hooks": [{"type": "command", "command": "other-hook"}]}
    ]
  }
}
`)
	if err := os.WriteFile(hooksPath, originalHooks, 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME=", "WSL_INTEROP=/run/WSL/1_interop", "PATH="+localBin+":"+fakeBin+":"+os.Getenv("PATH"))

	runScript(t, env, installScript, "--helper", helperPath, "--windows-exe", windowsPath)
	unchanged, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, originalHooks) {
		t.Fatal("install without --install-codex-hooks changed hooks.json")
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "lib", "enterlight", "codex-hook-wsl")); !os.IsNotExist(err) {
		t.Fatal("install without --install-codex-hooks created the hook wrapper")
	}
	runScript(t, env, uninstallScript)
	unchanged, err = os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, originalHooks) {
		t.Fatal("uninstall without recorded hook ownership changed hooks.json")
	}
	runScript(t, env, installScript, "--helper", helperPath, "--windows-exe", windowsPath)

	runScript(t, env, installScript, "--helper", helperPath, "--windows-exe", windowsPath, "--install-codex-hooks")
	wrapper := filepath.Join(home, ".local", "lib", "enterlight", "codex-hook-wsl")
	info, err := os.Stat(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("wrapper mode = %o", info.Mode().Perm())
	}
	backup, err := os.ReadFile(hooksPath + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backup, originalHooks) {
		t.Fatal("hooks backup does not match the pre-install file")
	}
	assertManagedHooks(t, hooksPath, wrapper)
	configData, err := os.ReadFile(filepath.Join(home, ".config", "enterlight", "wsl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(configData, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.CodexHooksManaged {
		t.Fatalf("hook ownership was not recorded: %#v", cfg)
	}

	// Reinstalling with the flag is idempotent, and omitting it later preserves
	// the recorded ownership without rewriting hooks.json.
	runScript(t, env, installScript, "--helper", helperPath, "--windows-exe", windowsPath, "--install-codex-hooks")
	assertManagedHooks(t, hooksPath, wrapper)
	beforeNoFlag, _ := os.ReadFile(hooksPath)
	runScript(t, env, installScript, "--helper", helperPath, "--windows-exe", windowsPath)
	afterNoFlag, _ := os.ReadFile(hooksPath)
	if !bytes.Equal(beforeNoFlag, afterNoFlag) {
		t.Fatal("reinstall without the hook flag rewrote hooks.json")
	}

	for _, state := range []string{"working", "waiting", "done", "error", "off"} {
		cmd := exec.Command(wrapper, state)
		cmd.Env = append(env, "POWERSHELL_EXIT_CODE=126")
		if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
			t.Fatalf("wrapper %s: err=%v output=%q", state, err, output)
		}
	}
	for _, code := range []string{"0", "42", "124", "137"} {
		cmd := exec.Command(wrapper, "working")
		cmd.Env = append(env, "POWERSHELL_EXIT_CODE="+code)
		if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
			t.Fatalf("wrapper exit %s: err=%v output=%q", code, err, output)
		}
	}
	for _, args := range [][]string{nil, {"idle"}, {"working", "extra"}} {
		cmd := exec.Command(wrapper, args...)
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); exitCode(err) != 2 || len(output) != 0 {
			t.Fatalf("wrapper invalid args %q: err=%v output=%q", args, err, output)
		}
	}
	started := time.Now()
	cmd := exec.Command(wrapper, "working")
	cmd.Env = append(env, "POWERSHELL_DELAY=1")
	if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("timed wrapper: err=%v output=%q", err, output)
	}
	if elapsed := time.Since(started); elapsed >= 4*time.Second {
		t.Fatalf("timed wrapper took %s", elapsed)
	}

	runScript(t, env, uninstallScript)
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Fatalf("managed wrapper remains: %v", err)
	}
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "codex-hook") || strings.Contains(string(data), "codex-hook-wsl") || !strings.Contains(string(data), "other-hook") || !strings.Contains(string(data), "keep-me") {
		t.Fatalf("selective hook uninstall failed: %s", data)
	}

	if err := os.MkdirAll(filepath.Dir(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, wrapper, "#!/bin/sh\necho keep-me\n")
	cmd = exec.Command("bash", installScript, "--helper", helperPath, "--windows-exe", windowsPath, "--install-codex-hooks")
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "Refusing to overwrite") {
		t.Fatalf("unmanaged hook wrapper was not protected: err=%v output=%s", err, output)
	}
}

func runScript(t *testing.T, env []string, script string, args ...string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", filepath.Base(script), err, output)
	}
}

func assertManagedHooks(t *testing.T, path, wrapper string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if root["custom"] != "keep-me" {
		t.Fatalf("unrelated root value was lost: %#v", root)
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks = %#v", root["hooks"])
	}
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "PermissionRequest", "Stop"} {
		groups, ok := hooks[event].([]any)
		if !ok {
			t.Fatalf("%s groups = %#v", event, hooks[event])
		}
		wantGroups := 1
		if event == "PreToolUse" {
			wantGroups = 2
		}
		if len(groups) != wantGroups {
			t.Fatalf("%s has %d groups, want %d", event, len(groups), wantGroups)
		}
		encoded, _ := json.Marshal(groups)
		if !bytes.Contains(encoded, []byte(wrapper)) || !bytes.Contains(encoded, []byte(`"commandWindows"`)) || !bytes.Contains(encoded, []byte(`C:\\Program Files\\Enterlight\\enterlight.exe\" codex-hook`)) {
			t.Fatalf("%s managed handler = %s", event, encoded)
		}
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
