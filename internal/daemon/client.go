package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var daemonHTTP = &http.Client{Timeout: 2 * time.Second}

func SetState(ctx context.Context, state string, autoStart bool) error {
	err := postJSON(ctx, "/set", SetRequest{State: state}, nil)
	if err == nil || !autoStart {
		return err
	}
	if startErr := startDaemonDetached(); startErr != nil {
		return fmt.Errorf("daemon unavailable (%v), and failed to start it: %w", err, startErr)
	}
	var last error
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if last = postJSON(ctx, "/set", SetRequest{State: state}, nil); last == nil {
			return nil
		}
	}
	return fmt.Errorf("daemon did not become ready: %w", last)
}

func GetStatus(ctx context.Context) (Status, error) {
	var status Status
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+Address+"/status", nil)
	if err != nil {
		return status, err
	}
	resp, err := daemonHTTP.Do(req)
	if err != nil {
		return status, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return status, fmt.Errorf("daemon HTTP %s: %s", resp.Status, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return status, err
	}
	return status, nil
}

func Stop(ctx context.Context) error {
	return postJSON(ctx, "/shutdown", map[string]any{}, nil)
}

func postJSON(ctx context.Context, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+Address+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := daemonHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("daemon HTTP %s: %s", resp.Status, string(data))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func startDaemonDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	configureDetachedProcess(cmd)
	return cmd.Start()
}
