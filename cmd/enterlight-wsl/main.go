//go:build linux

package main

import (
	"fmt"
	"os"

	"github.com/wakadorimk2/enterlight/internal/wsladapter"
)

func main() {
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

func fail(err error) {
	fmt.Fprintln(os.Stderr, "enterlight WSL adapter:", err)
	os.Exit(1)
}
