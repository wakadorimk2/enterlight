//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/wakadorimk2/enterlight/internal/codex"
	"github.com/wakadorimk2/enterlight/internal/wsladapter"
)

func main() {
	if os.Getenv("ENTERLIGHT_WSL_INTERNAL_MANAGE_HOOKS") == "1" && len(os.Args) > 1 {
		switch os.Args[1] {
		case "install-codex-hooks":
			if len(os.Args) != 5 {
				fail(fmt.Errorf("usage: install-codex-hooks <hooks-path> <wrapper> <windows-exe>"))
			}
			commandBase := quoteCommand(os.Args[3]) + " "
			commandWindowsBase := quoteCommand(os.Args[4]) + " codex-hook "
			if err := codex.InstallAt(os.Args[2], commandBase, commandWindowsBase); err != nil {
				fail(err)
			}
			return
		case "uninstall-codex-hooks":
			if len(os.Args) != 3 {
				fail(fmt.Errorf("usage: uninstall-codex-hooks <hooks-path>"))
			}
			if err := codex.UninstallAt(os.Args[2]); err != nil {
				fail(err)
			}
			return
		}
	}

	configPath := os.Getenv("ENTERLIGHT_WSL_CONFIG")
	if configPath == "" {
		var err error
		configPath, err = wsladapter.DefaultConfigPath()
		if err != nil {
			fail(err)
		}
	}
	cfg, err := wsladapter.LoadConfig(configPath)
	if err != nil {
		fail(err)
	}
	code, err := wsladapter.Run(cfg, os.Args[1:])
	if err != nil {
		fail(err)
	}
	os.Exit(code)
}

func quoteCommand(path string) string {
	if strings.ContainsAny(path, " \t\"") {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return path
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "enterlight WSL adapter:", err)
	os.Exit(1)
}
