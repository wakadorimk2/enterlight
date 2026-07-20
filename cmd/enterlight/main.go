package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wakadorimk2/enterlight/internal/chroma"
	"github.com/wakadorimk2/enterlight/internal/codex"
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
		// Codex sends lifecycle JSON on stdin. Drain it so the hook pipe closes cleanly;
		// the selected lifecycle event is all Enterlight needs for v0.1.
		_, _ = io.Copy(io.Discard, io.LimitReader(os.Stdin, 1<<20))
		return setState(args[1], true)
	case "set":
		if len(args) < 2 {
			return errors.New("usage: enterlight set <working|waiting|done|error|off>")
		}
		return setState(args[1], false)
	case "working", "waiting", "done", "error", "off":
		return setState(cmd, false)
	default:
		return fmt.Errorf("unknown command %q; run 'enterlight help'", cmd)
	}
}

func setState(state string, quiet bool) error {
	state = strings.ToLower(strings.TrimSpace(state))
	switch state {
	case "working", "waiting", "done", "error", "off":
	default:
		return fmt.Errorf("unknown state %q", state)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := daemon.SetState(ctx, state, true); err != nil {
		return err
	}
	if !quiet {
		fmt.Println(state)
	}
	return nil
}

func doctor() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := chroma.New()
	v, err := c.Version(ctx)
	if err != nil {
		fmt.Println("Razer Chroma SDK: unavailable")
		fmt.Println("  Make sure Razer Synapse and Chroma Connect/SDK Service are installed and running.")
		return err
	}
	fmt.Println("Razer Chroma SDK: available")
	fmt.Println("Version response:", v)
	status, err := daemon.GetStatus(ctx)
	if err != nil {
		fmt.Println("Enterlight daemon: stopped (it will auto-start on first state command)")
	} else {
		fmt.Printf("Enterlight daemon: running, state=%s, connected=%v\n", status.State, status.ChromaConnected)
	}
	return nil
}

func printHelp() {
	fmt.Printf(`Enterlight %s — light only the Enter key as an agent status beacon.

Usage:
  enterlight working           Blue, solid
  enterlight waiting           Amber, breathing
  enterlight done              Green, three pulses, then restore Synapse
  enterlight error             Red, blinking
  enterlight off               Restore the normal Synapse profile

Commands:
  enterlight set <state>        Set a state
  enterlight status             Show daemon status
  enterlight doctor             Check the Razer Chroma SDK
  enterlight install-codex      Add lifecycle hooks to ~/.codex/hooks.json
  enterlight uninstall-codex    Remove only Enterlight's Codex hooks
  enterlight stop-daemon        Stop the local daemon
  enterlight version            Print version

The local daemon starts automatically on the first state command.
`, version)
}
