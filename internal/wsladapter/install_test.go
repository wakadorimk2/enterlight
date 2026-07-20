//go:build linux

package wsladapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	env := append(os.Environ(), "HOME="+home, "WSL_INTEROP=/run/WSL/1_interop", "PATH="+localBin+":"+fakeBin+":"+os.Getenv("PATH"))

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
	if cfg.CodexPath != codexPath || cfg.WindowsBinary != windowsPath || cfg.CodexVersion != SupportedCodexVersion {
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
	wrongOrderEnv := append(os.Environ(), "HOME="+home, "WSL_INTEROP=/run/WSL/1_interop", "PATH="+fakeBin+":"+localBin+":"+os.Getenv("PATH"))
	cmd = exec.Command("bash", installScript, "--helper", helperPath, "--windows-exe", windowsPath)
	cmd.Env = wrongOrderEnv
	if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "managed shim would not run") {
		t.Fatalf("PATH precedence was not rejected: err=%v output=%s", err, output)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
