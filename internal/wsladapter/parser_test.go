package wsladapter

import (
	"reflect"
	"strings"
	"testing"
)

const commandPrompt = "\x1b[2J\x1b[HWould you like to run the following command?\r\n\r\n› 1. Yes, proceed (y)\r\n  2. Yes, and don't ask again for this command in this session (a)\r\n  3. No, continue without running it (n)\r\n  4. No, and tell Codex what to do differently (esc)\r\n\r\nPress enter to confirm or esc to cancel\r\n"

func TestParserRecognizesFragmentedApprovalAndSelection(t *testing.T) {
	p := NewParser()
	var got *Snapshot
	for _, b := range []byte(commandPrompt) {
		got = p.Feed([]byte{b})
	}
	want := &Snapshot{Candidates: []Candidate{{1, "allow_once"}, {2, "allow_session"}, {3, "decline"}, {4, "cancel"}}, SelectedKey: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %#v, want %#v", got, want)
	}

	// Codex redraws just the two affected rows when the selection moves.
	got = p.Feed([]byte("\x1b[3;1H  1. Yes, proceed (y)\x1b[K\x1b[4;1H› 2. Yes, and don't ask again for this command in this session (a)\x1b[K"))
	if got == nil || got.SelectedKey != 2 {
		t.Fatalf("selection redraw = %#v", got)
	}
}

func TestParserWaitsForCompleteCandidateList(t *testing.T) {
	fixture := strings.Replace(commandPrompt, approvalFooter+"\r\n", "", 1)
	if got := NewParser().Feed([]byte(fixture)); got != nil { t.Fatalf("partial prompt produced snapshot: %#v", got) }
}

func TestParserFailClosedCases(t *testing.T) {
	tests := map[string]string{
		"unknown label":  strings.Replace(commandPrompt, "Yes, proceed", "Yes, probably", 1),
		"duplicate":      strings.Replace(commandPrompt, "2. Yes", "1. Yes", 1),
		"out of range":   strings.Replace(commandPrompt, "2. Yes", "10. Yes", 1),
		"missing":        strings.Replace(commandPrompt, "  2. Yes, and don't ask again for this command in this session (a)\r\n", "", 1),
		"no selection":   strings.Replace(commandPrompt, "› 1.", "  1.", 1),
		"two selections": strings.Replace(commandPrompt, "  2.", "› 2.", 1),
		"two prompts":    commandPrompt + "\r\nWould you like to make the following edits?\r\n",
		"dynamic prefix": strings.Replace(commandPrompt, "Yes, and don't ask again for this command in this session", "Yes, and don't ask again for commands that start with `curl`", 1),
	}
	for name, fixture := range tests {
		t.Run(name, func(t *testing.T) {
			if got := NewParser().Feed([]byte(fixture)); got != nil {
				t.Fatalf("unexpected snapshot: %#v", got)
			}
		})
	}
}

func TestParserClearsWhenPromptLeavesScreen(t *testing.T) {
	p := NewParser()
	if got := p.Feed([]byte(commandPrompt)); got == nil {
		t.Fatal("fixture was not recognized")
	}
	if got := p.Feed([]byte("\x1b[2J\x1b[Hready")); got != nil {
		t.Fatalf("stale snapshot: %#v", got)
	}
}

func TestParserDoesNotRetainUnrelatedOutput(t *testing.T) {
	p := NewParser()
	secret := "typed-or-command-secret"
	p.Feed([]byte(secret + "\r\n" + commandPrompt))
	for _, line := range p.rows {
		if strings.Contains(string(line.cells), secret) {
			t.Fatal("unrelated terminal text was retained")
		}
	}
}

func TestAllFixedDecisionClasses(t *testing.T) {
	fixture := "\x1b[2J\x1b[HWould you like to grant these permissions?\r\n› 1. Yes, grant these permissions for this turn (y)\r\n  2. Yes, grant these permissions for this session (a)\r\n  3. No, continue without permissions (n)\r\nPress enter to confirm or esc to cancel\r\n"
	got := NewParser().Feed([]byte(fixture))
	if got == nil {
		t.Fatal("permissions prompt was not recognized")
	}
	want := []Candidate{{1, "allow_once"}, {2, "allow_session"}, {3, "decline"}}
	if !reflect.DeepEqual(got.Candidates, want) {
		t.Fatalf("candidates = %#v", got.Candidates)
	}
}

func TestMCPApprovalTitleDoesNotRetainServerName(t *testing.T) {
	p := NewParser()
	fixture := "\x1b[2J\x1b[Hprivate-server-name needs your approval.\r\n› 1. Yes, provide the requested info (y)\r\n  2. No, but continue without it (n)\r\n  3. Cancel this request (esc)\r\nPress enter to confirm or esc to cancel\r\n"
	got := p.Feed([]byte(fixture))
	if got == nil || got.SelectedKey != 1 {
		t.Fatalf("MCP snapshot = %#v", got)
	}
	for _, line := range p.rows {
		if strings.Contains(string(line.cells), "private-server-name") {
			t.Fatal("MCP server name was retained")
		}
	}
}
