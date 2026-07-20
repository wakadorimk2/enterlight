//go:build linux

package wsladapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBridgeSetsRenewsAndClearsLease(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "bridge.log")
	executable := filepath.Join(root, "enterlight.exe")
	writeExecutable(t, executable, "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$BRIDGE_TEST_LOG\"\n")
	t.Setenv("BRIDGE_TEST_LOG", logPath)

	b := newBridge(executable, "adapter-test")
	b.Update(&Snapshot{Candidates: []Candidate{{Key: 1, Decision: "allow_once"}, {Key: 2, Decision: "cancel"}}, SelectedKey: 1})
	waitForFileContains(t, logPath, "approval set", time.Second)
	time.Sleep(refreshInterval + 100*time.Millisecond)
	b.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "approval set") < 2 {
		t.Fatalf("lease was not renewed: %s", text)
	}
	for _, want := range []string{"--lease 5000", "--candidate 1=allow_once", "--candidate 2=cancel", "approval clear --session adapter-test"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
}

func waitForFileContains(t *testing.T, path, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), needle) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q was not written to %s", needle, path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
