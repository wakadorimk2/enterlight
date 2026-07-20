//go:build linux

package wsladapter

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func Run(cfg Config, args []string) (int, error) {
	helperPath, err := os.Executable()
	if err != nil {
		return 1, err
	}
	if err := ValidateExecutables(cfg, helperPath); err != nil {
		return 1, err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return runDirect(cfg.CodexPath, args)
	}
	return runPTY(cfg, args)
}

func runDirect(codexPath string, args []string) (int, error) {
	cmd := exec.Command(codexPath, args...)
	cmd.Env = os.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return processExitCode(exitErr), nil
	}
	return 1, err
}

func runPTY(cfg Config, args []string) (int, error) {

	cmd := exec.Command(cfg.CodexPath, args...)
	cmd.Env = os.Environ()
	childPTY, err := pty.Start(cmd)
	if err != nil {
		return 1, err
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
		if rawErr != nil {
			_ = childPTY.Close()
			_ = cmd.Process.Kill()
			return 1, rawErr
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)
		_ = pty.InheritSize(os.Stdin, childPTY)
	}

	b := newBridge(cfg.WindowsBinary, randomSessionID())
	defer b.Close()
	parser := NewParser()

	outputDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := childPTY.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				if _, writeErr := os.Stdout.Write(chunk); writeErr != nil {
					outputDone <- writeErr
					return
				}
				b.Update(parser.Feed(chunk))
			}
			if readErr != nil {
				outputDone <- readErr
				return
			}
		}
	}()
	go func() { _, _ = io.Copy(childPTY, os.Stdin) }()

	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()
	go func() {
		for sig := range sigCh {
			if sig == syscall.SIGWINCH {
				_ = pty.InheritSize(os.Stdin, childPTY)
				continue
			}
			_ = cmd.Process.Signal(sig)
		}
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-waitDone:
		<-outputDone
	case <-outputDone:
		// A broken outer stdout must not leave a child blocked on a full PTY.
		_ = cmd.Process.Kill()
		waitErr = <-waitDone
	}
	_ = childPTY.Close()
	if waitErr == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return processExitCode(exitErr), nil
	}
	return 1, waitErr
}

func processExitCode(exitErr *exec.ExitError) int {
	if status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exitErr.ExitCode()
}

func randomSessionID() string {
	var data [12]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "wsl-codex"
	}
	return "wsl-codex-" + hex.EncodeToString(data[:])
}
