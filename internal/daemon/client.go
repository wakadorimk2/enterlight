package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var daemonHTTP = &http.Client{Timeout: 2 * time.Second}

type httpStatusError struct {
	status string
	body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("daemon HTTP %s: %s", e.status, e.body)
}

func SetState(ctx context.Context, state string, autoStart bool) error {
	return SetSessionState(ctx, SetRequest{State: state}, autoStart)
}

func SetSessionState(ctx context.Context, request SetRequest, autoStart bool) error {
	return postWithAutoStart(ctx, "/sessions/update", request, autoStart)
}

func SetVisibility(ctx context.Context, request VisibilityRequest, autoStart bool) error {
	return postWithAutoStart(ctx, "/sessions/visibility", request, autoStart)
}

func SetApproval(ctx context.Context, request ApprovalRequest, autoStart bool) error {
	return postWithAutoStart(ctx, "/approval", request, autoStart)
}

func ClearApproval(ctx context.Context, request ApprovalClearRequest, autoStart bool) error {
	return requestWithAutoStart(ctx, http.MethodDelete, "/approval", request, autoStart)
}

func SetPreset(ctx context.Context, preset string, autoStart bool) error {
	return postWithAutoStart(ctx, "/preset", PresetRequest{Preset: preset}, autoStart)
}

func postWithAutoStart(ctx context.Context, path string, payload any, autoStart bool) error {
	return requestWithAutoStart(ctx, http.MethodPost, path, payload, autoStart)
}

func requestWithAutoStart(ctx context.Context, method, path string, payload any, autoStart bool) error {
	err := requestJSON(ctx, method, path, payload, nil)
	if err == nil || !autoStart {
		return err
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return err
	}
	if startErr := startDaemonDetached(); startErr != nil {
		return fmt.Errorf("daemon unavailable (%v), and failed to start it: %w", err, startErr)
	}
	var last error
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if last = requestJSON(ctx, method, path, payload, nil); last == nil {
			return nil
		}
		if errors.As(last, &statusErr) {
			return last
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
	return requestJSON(ctx, http.MethodPost, path, payload, out)
}

func requestJSON(ctx context.Context, method, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+Address+path, bytes.NewReader(body))
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
		return &httpStatusError{status: resp.Status, body: string(data)}
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
