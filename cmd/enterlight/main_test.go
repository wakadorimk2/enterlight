package main

import (
	"strings"
	"testing"

	"github.com/wakadorimk2/enterlight/internal/chroma"
)

func TestRecoveryAdvice(t *testing.T) {
	advice := strings.Join(recoveryAdvice(chroma.ErrorClientLimit), " ")
	for _, want := range []string{"Stop the daemon", "15 seconds", "restart Synapse"} {
		if !strings.Contains(advice, want) {
			t.Fatalf("client-limit advice %q does not contain %q", advice, want)
		}
	}
}
