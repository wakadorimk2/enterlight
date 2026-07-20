//go:build linux

package wsladapter

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPTYProxyPreservesProcessContract(t *testing.T) {
	root := t.TempDir()
	codexPath := filepath.Join(root, "real-codex")
	writeExecutable(t, codexPath, `#!/bin/sh
printf 'args=%s|%s\n' "$1" "$2"
printf 'env=%s\n' "$PTY_TEST_ENV"
printf 'cwd=%s\n' "$PWD"
IFS= read -r line
printf 'input=%s\n' "$line"
exit 23
`)
	windowsPath := filepath.Join(root, "enterlight.exe")
	writeExecutable(t, windowsPath, "#!/bin/sh\nexit 9\n")

	oldIn, oldOut := os.Stdin, os.Stdout
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = inputRead, outputWrite
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	if _, err := inputWrite.WriteString("hello from stdin\n"); err != nil {
		t.Fatal(err)
	}
	_ = inputWrite.Close()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	t.Setenv("PTY_TEST_ENV", "preserved")

	code, runErr := runPTY(Config{CodexPath: codexPath, CodexVersion: SupportedCodexVersion, WindowsBinary: windowsPath}, []string{"one", "two words"})
	_ = outputWrite.Close()
	output, _ := io.ReadAll(outputRead)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if code != 23 {
		t.Fatalf("exit code = %d, want 23", code)
	}
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	for _, want := range []string{"args=one|two words", "env=preserved", "cwd=" + root, "input=hello from stdin"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}

func TestValidateExecutablesRejectsRecursion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "same")
	writeExecutable(t, path, "#!/bin/sh\n")
	err := ValidateExecutables(Config{CodexPath: path, CodexVersion: SupportedCodexVersion, WindowsBinary: path}, path)
	if err == nil || !strings.Contains(err.Error(), "recursive") {
		t.Fatalf("error = %v", err)
	}
}

func TestPTYProxyForwardsSignals(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(root, "ready")
	codexPath := filepath.Join(root, "real-codex")
	writeExecutable(t, codexPath, "#!/bin/sh\ntrap 'exit 42' TERM\ntouch '"+ready+"'\nwhile :; do :; done\n")
	windowsPath := filepath.Join(root, "enterlight.exe")
	writeExecutable(t, windowsPath, "#!/bin/sh\nexit 0\n")

	result := make(chan int, 1)
	errResult := make(chan error, 1)
	go func() {
		code, err := runPTY(Config{CodexPath: codexPath, CodexVersion: SupportedCodexVersion, WindowsBinary: windowsPath}, nil)
		result <- code
		errResult <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-result:
		t.Fatalf("resize signal terminated child with %d", code)
	case <-time.After(50 * time.Millisecond):
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-result:
		if err := <-errResult; err != nil {
			t.Fatal(err)
		}
		if code != 42 {
			t.Fatalf("exit code = %d, want 42", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("termination signal was not forwarded")
	}
}
