package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wakadorimk2/enterlight/internal/chroma"
	"github.com/wakadorimk2/enterlight/internal/codex"
	enterconfig "github.com/wakadorimk2/enterlight/internal/config"
	"github.com/wakadorimk2/enterlight/internal/daemon"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "enterlight:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}
	cmd := strings.ToLower(args[0])
	switch cmd {
	case "help", "-h", "--help":
		printHelp()
		return nil
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "daemon":
		return daemon.NewServer(version).Run(context.Background())
	case "status":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		status, err := daemon.GetStatus(ctx)
		if err != nil {
			return errors.New("daemon is not running")
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(data))
		return nil
	case "stop-daemon":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := daemon.Stop(ctx); err != nil {
			return errors.New("daemon is not running")
		}
		fmt.Println("daemon stopped")
		return nil
	case "doctor":
		return doctor()
	case "install-codex":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, _ = filepath.Abs(exe)
		path, err := codex.Install(exe)
		if err != nil {
			return err
		}
		fmt.Printf("installed Codex hooks: %s\n", path)
		fmt.Println("Open /hooks in Codex once to review and trust them.")
		return nil
	case "uninstall-codex":
		path, err := codex.Uninstall()
		if err != nil {
			return err
		}
		fmt.Printf("removed Enterlight hooks from: %s\n", path)
		return nil
	case "codex-hook":
		if len(args) < 2 {
			return errors.New("usage: enterlight codex-hook <working|waiting|done|error|off>")
		}
		data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		var input struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(data, &input)
		if input.SessionID == "" {
			input.SessionID = "codex"
		}
		return setSessionState(input.SessionID, args[1], nil, true)
	case "set":
		if len(args) < 2 {
			return errors.New("usage: enterlight set <working|waiting|done|error|off>")
		}
		return setState(args[1], false)
	case "session":
		if len(args) != 3 {
			return errors.New("usage: enterlight session <id> <working|waiting|done|error|idle|off>")
		}
		return setSessionState(args[1], args[2], nil, false)
	case "visible":
		if len(args) != 3 {
			return errors.New("usage: enterlight visible <session-id> <true|false>")
		}
		visible, err := strconv.ParseBool(args[2])
		if err != nil {
			return errors.New("visibility must be true or false")
		}
		return setVisibility(args[1], visible)
	case "approval":
		return approvalCommand(args[1:])
	case "preset":
		if len(args) != 2 || !enterconfig.ValidPreset(args[1]) {
			return errors.New("usage: enterlight preset <calm|vivid|max>")
		}
		path, err := enterconfig.Path()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := daemon.SetPreset(ctx, args[1], true); err != nil {
			return err
		}
		fmt.Printf("preset %s (%s)\n", args[1], path)
		return nil
	case "working", "waiting", "done", "error", "idle", "off":
		return setState(cmd, false)
	default:
		return fmt.Errorf("unknown command %q; run 'enterlight help'", cmd)
	}
}

func setState(state string, quiet bool) error {
	var visible *bool
	if strings.EqualFold(strings.TrimSpace(state), "idle") {
		value := true
		visible = &value
	}
	return setSessionState("manual", state, visible, quiet)
}

func setSessionState(sessionID, state string, visible *bool, quiet bool) error {
	state = strings.ToLower(strings.TrimSpace(state))
	switch state {
	case "working", "waiting", "done", "error", "idle", "off":
	default:
		return fmt.Errorf("unknown state %q", state)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := daemon.SetSessionState(ctx, daemon.SetRequest{SessionID: sessionID, State: state, Visible: visible}, true); err != nil {
		return err
	}
	if !quiet {
		fmt.Println(state)
	}
	return nil
}

func setVisibility(sessionID string, visible bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := daemon.SetVisibility(ctx, daemon.VisibilityRequest{SessionID: sessionID, Visible: visible}, true); err != nil {
		return err
	}
	fmt.Printf("%s visible=%t\n", sessionID, visible)
	return nil
}

func approvalCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: enterlight approval <set|clear> [options]")
	}
	action := args[0]
	var sessionID string
	selected := 0
	leaseMS := 0
	var candidates []daemon.ApprovalCandidate
	for i := 1; i < len(args); i++ {
		if i+1 >= len(args) {
			return fmt.Errorf("missing value for %s", args[i])
		}
		name, value := args[i], args[i+1]
		i++
		switch name {
		case "--session":
			sessionID = value
		case "--selected":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return errors.New("--selected must be a number")
			}
			selected = parsed
		case "--lease":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return errors.New("--lease must be milliseconds")
			}
			leaseMS = parsed
		case "--candidate":
			parts := strings.SplitN(value, "=", 2)
			if len(parts) != 2 {
				return errors.New("--candidate must use <number>=<decision>")
			}
			key, err := strconv.Atoi(parts[0])
			if err != nil {
				return errors.New("candidate key must be a number")
			}
			candidates = append(candidates, daemon.ApprovalCandidate{Key: key, Decision: parts[1]})
		default:
			return fmt.Errorf("unknown approval option %q", name)
		}
	}
	if sessionID == "" {
		return errors.New("--session is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch action {
	case "set":
		err := daemon.SetApproval(ctx, daemon.ApprovalRequest{SessionID: sessionID, Candidates: candidates, SelectedKey: selected, LeaseMS: leaseMS}, true)
		if err == nil {
			fmt.Println("approval overlay set")
		}
		return err
	case "clear":
		err := daemon.ClearApproval(ctx, daemon.ApprovalClearRequest{SessionID: sessionID}, true)
		if err == nil {
			fmt.Println("approval overlay cleared")
		}
		return err
	default:
		return errors.New("usage: enterlight approval <set|clear> [options]")
	}
}

func doctor() error {
	sdkCtx, sdkCancel := context.WithTimeout(context.Background(), 3*time.Second)
	c := chroma.New()
	v, sdkErr := c.Version(sdkCtx)
	sdkCancel()
	if sdkErr != nil {
		fmt.Println("Razer Chroma SDK: unavailable")
		fmt.Println("  Make sure Razer Synapse and Chroma Connect/SDK Service are installed and running.")
	} else {
		fmt.Println("Razer Chroma SDK: available")
		fmt.Println("Version response:", v)
	}
	statusCtx, statusCancel := context.WithTimeout(context.Background(), 2*time.Second)
	status, err := daemon.GetStatus(statusCtx)
	statusCancel()
	if err != nil {
		fmt.Println("Enterlight daemon: stopped (it will auto-start on first state command)")
	} else {
		fmt.Printf("Enterlight daemon: running, state=%s, connected=%v\n", status.State, status.ChromaConnected)
		if status.LastError != "" {
			fmt.Printf("Last error: %s\n", status.LastError)
			for _, line := range recoveryAdvice(status.LastErrorCode) {
				fmt.Println(" ", line)
			}
		}
	}
	return sdkErr
}

func recoveryAdvice(code string) []string {
	switch code {
	case chroma.ErrorSessionUnreachable:
		return []string{
			"The SDK returned a session that Enterlight could not reach.",
			"Stop the daemon, wait at least 15 seconds, then try a state command again.",
		}
	case chroma.ErrorClientLimit:
		return []string{
			"The Chroma REST client limit appears to be full.",
			"Stop the daemon and wait at least 15 seconds for old sessions to expire.",
			"Only if that does not help, restart Synapse or the Razer Chroma SDK Service.",
		}
	case chroma.ErrorUnavailable:
		return []string{
			"Check that Razer Synapse and the Razer Chroma SDK Service are running.",
		}
	default:
		return []string{"Run 'enterlight stop-daemon', wait at least 15 seconds, and try again."}
	}
}

func printHelp() {
	fmt.Printf(`Enterlight %s — animate a Razer keyboard as an agent status surface.

Usage:
  enterlight working           Cyan/purple wave
  enterlight waiting           Amber, breathing
  enterlight done              Green/white completion burst
  enterlight error             Red/orange warning
  enterlight idle              Low-distraction ambient scene
  enterlight off               Restore the normal Synapse profile

Commands:
  enterlight set <state>        Set a state
  enterlight session <id> <state>
                                Set one of up to four displayed sessions
  enterlight visible <id> <bool>
                                Control whether an idle session is visible
  enterlight preset <name>      Persist calm, vivid, or max presentation
  enterlight approval set --session <id> --selected <n>
      --candidate <n>=<allow_once|allow_session|decline|cancel> [...]
  enterlight approval clear --session <id>
  enterlight status             Show daemon status
  enterlight doctor             Check the Razer Chroma SDK
  enterlight install-codex      Add lifecycle hooks to ~/.codex/hooks.json
  enterlight uninstall-codex    Remove only Enterlight's Codex hooks
  enterlight stop-daemon        Stop the local daemon
  enterlight version            Print version

The local daemon starts automatically on the first state command.
`, version)
}
