package wsladapter

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Snapshot is the only information extracted from the terminal stream. It
// deliberately contains no command, conversation, or user input text.
type Snapshot struct {
	Candidates  []Candidate
	SelectedKey int
}

type Candidate struct {
	Key      int
	Decision string
}

// approvalLabels is the fixed label set rendered by Codex CLI 0.144.6. Labels
// containing request data (for example, a proposed command prefix) are
// intentionally absent and therefore fail closed.
var approvalLabels = map[string]string{
	"Yes, proceed":        "allow_once",
	"Yes, just this once": "allow_once",
	"Yes, grant these permissions for this turn":                "allow_once",
	"Yes, grant for this turn with strict auto review":          "allow_once",
	"Yes, provide the requested info":                           "allow_once",
	"Yes, and don't ask again for this command in this session": "allow_session",
	"Yes, and allow this host for this conversation":            "allow_session",
	"Yes, grant these permissions for this session":             "allow_session",
	"Yes, and allow this host in the future":                    "allow_session",
	"Yes, and don't ask again for these files":                  "allow_session",
	"No, continue without running it":                           "decline",
	"No, continue without permissions":                          "decline",
	"No, but continue without it":                               "decline",
	"No, and tell Codex what to do differently":                 "cancel",
	"Cancel this request":                                       "cancel",
}

var approvalTitles = []string{
	"Would you like to run the following command?",
	"Would you like to grant these permissions?",
	"Would you like to make the following edits?",
}

var mcpApprovalSuffix = []rune(" needs your approval.")

const approvalFooter = "Press enter to confirm or esc to cancel"

var shortcutSuffix = regexp.MustCompile(` \([^()]{1,20}\)$`)

type candidateLine struct {
	key      int
	decision string
	selected bool
}

type trackedLine struct {
	cells            []rune
	active           bool
	prompt           bool
	footer           bool
	invalidCandidate bool
	mcpSuffixMatch   int
}

// Parser is a deliberately small terminal-screen observer. It tracks cursor
// movement, but retains a row only while it can still be an approval title or
// an allowlisted candidate. Arbitrary terminal output is discarded as soon as
// it differs from those prefixes.
type Parser struct {
	rows      map[int]*trackedLine
	row       int
	col       int
	escape    []byte
	utf8Bytes []byte
	last      *Snapshot
}

func NewParser() *Parser {
	return &Parser{rows: make(map[int]*trackedLine)}
}

// Feed consumes a possibly fragmented ANSI/UTF-8 output chunk. The returned
// snapshot is nil unless exactly one prompt, unique 1-9 candidates, and exactly
// one selected candidate are visible.
func (p *Parser) Feed(data []byte) *Snapshot {
	for _, b := range data {
		p.consumeByte(b)
	}
	p.last = p.snapshot()
	return cloneSnapshot(p.last)
}

func (p *Parser) consumeByte(b byte) {
	if len(p.escape) > 0 {
		p.escape = append(p.escape, b)
		if len(p.escape) == 2 && b != '[' && b != ']' {
			p.escape = nil
			return
		}
		if len(p.escape) >= 2 && p.escape[1] == ']' {
			if b == 0x07 || (len(p.escape) >= 2 && p.escape[len(p.escape)-2] == 0x1b && b == '\\') {
				p.escape = nil
			}
			return
		}
		if len(p.escape) >= 3 && p.escape[1] == '[' && b >= 0x40 && b <= 0x7e {
			p.handleCSI(string(p.escape[2:len(p.escape)-1]), b)
			p.escape = nil
		}
		if len(p.escape) > 128 {
			p.escape = nil
		}
		return
	}
	if b == 0x1b {
		p.flushInvalidUTF8()
		p.escape = []byte{b}
		return
	}
	switch b {
	case '\r':
		p.flushInvalidUTF8()
		p.col = 0
		return
	case '\n':
		p.flushInvalidUTF8()
		p.row++
		p.col = 0
		return
	case '\b':
		p.flushInvalidUTF8()
		if p.col > 0 {
			p.col--
		}
		return
	}
	if b < 0x20 || b == 0x7f {
		return
	}
	p.utf8Bytes = append(p.utf8Bytes, b)
	if utf8.FullRune(p.utf8Bytes) {
		r, size := utf8.DecodeRune(p.utf8Bytes)
		p.utf8Bytes = p.utf8Bytes[size:]
		if r != utf8.RuneError {
			p.writeRune(r)
		}
	}
}

func (p *Parser) flushInvalidUTF8() { p.utf8Bytes = nil }

func (p *Parser) writeRune(r rune) {
	if p.row < 0 || p.row > 4096 || p.col < 0 || p.col > 1024 {
		p.col++
		return
	}
	line := p.rows[p.row]
	if line == nil {
		line = &trackedLine{active: true}
		p.rows[p.row] = line
	}
	if p.col == 0 && (!line.active || line.prompt || line.footer) {
		*line = trackedLine{active: true}
	}
	line.mcpSuffixMatch = advanceSuffixMatch(line.mcpSuffixMatch, r)
	if line.mcpSuffixMatch == len(mcpApprovalSuffix) {
		line.cells = nil
		line.active = true
		line.prompt = true
		p.col++
		return
	}
	if line.prompt || line.footer {
		p.col++
		return
	}
	if !line.active {
		p.col++
		return
	}
	for len(line.cells) <= p.col {
		line.cells = append(line.cells, ' ')
	}
	line.cells[p.col] = r
	p.col++
	text := strings.TrimLeft(string(line.cells), " \t")
	if completedApprovalTitle(text) {
		line.cells = nil
		line.prompt = true
		return
	}
	if text == approvalFooter {
		line.cells = nil
		line.footer = true
		return
	}
	if !couldBeApprovalLine(string(line.cells)) {
		line.invalidCandidate = looksLikeCandidate(string(line.cells))
		line.cells = nil
		line.active = false
	}
}

func advanceSuffixMatch(state int, r rune) int {
	if state < len(mcpApprovalSuffix) && r == mcpApprovalSuffix[state] {
		return state + 1
	}
	if r == mcpApprovalSuffix[0] {
		return 1
	}
	return 0
}

func couldBeApprovalLine(raw string) bool {
	text := strings.TrimLeft(raw, " \t")
	if text == "" {
		return true
	}
	for _, title := range approvalTitles {
		if strings.HasPrefix(title, text) || strings.HasPrefix(text, title) {
			return true
		}
	}
	if strings.HasPrefix(approvalFooter, text) || strings.HasPrefix(text, approvalFooter) {
		return true
	}
	networkTitle := "Do you want to approve network access to \""
	if strings.HasPrefix(networkTitle, text) || strings.HasPrefix(text, networkTitle) {
		return true
	}
	if strings.HasPrefix(text, "›") {
		text = strings.TrimLeft(strings.TrimPrefix(text, "›"), " \t")
	}
	if text == "" {
		return true
	}
	if text[0] < '1' || text[0] > '9' {
		return false
	}
	text = text[1:]
	if text == "" {
		return true
	}
	if text[0] != '.' {
		return false
	}
	label := strings.TrimLeft(text[1:], " \t")
	if label == "" {
		return true
	}
	for allowed := range approvalLabels {
		if strings.HasPrefix(allowed, label) {
			return true
		}
		if strings.HasPrefix(label, allowed) && couldBeShortcut(label[len(allowed):]) {
			return true
		}
	}
	return false
}

func couldBeShortcut(rest string) bool {
	if rest == "" {
		return true
	}
	if strings.HasPrefix(" (", rest) {
		return true
	}
	if !strings.HasPrefix(rest, " (") {
		return false
	}
	if len(rest) > 24 {
		return false
	}
	if closeAt := strings.IndexByte(rest, ')'); closeAt >= 0 {
		return strings.TrimSpace(rest[closeAt+1:]) == ""
	}
	return true
}

func looksLikeCandidate(raw string) bool {
	text := strings.TrimLeft(raw, " \t")
	if strings.HasPrefix(text, "›") {
		text = strings.TrimLeft(strings.TrimPrefix(text, "›"), " \t")
	}
	return len(text) >= 1 && text[0] >= '0' && text[0] <= '9'
}

func completedApprovalTitle(text string) bool {
	for _, title := range approvalTitles {
		if text == title {
			return true
		}
	}
	return text == "Do you want to approve network access to \""
}

func (p *Parser) handleCSI(params string, final byte) {
	clean := strings.TrimLeft(params, "?")
	values := parseCSIValues(clean)
	mode := 0
	if len(values) > 0 {
		mode = values[0]
	}
	first := 1
	if len(values) > 0 && values[0] != 0 {
		first = values[0]
	}
	switch final {
	case 'A':
		p.row -= first
	case 'B':
		p.row += first
	case 'C':
		p.col += first
	case 'D':
		p.col -= first
		if p.col < 0 {
			p.col = 0
		}
	case 'E':
		p.row += first
		p.col = 0
	case 'F':
		p.row -= first
		p.col = 0
	case 'G':
		p.col = first - 1
	case 'd':
		p.row = first - 1
	case 'H', 'f':
		r, c := 1, 1
		if len(values) > 0 && values[0] > 0 {
			r = values[0]
		}
		if len(values) > 1 && values[1] > 0 {
			c = values[1]
		}
		p.row, p.col = r-1, c-1
	case 'J':
		if mode == 2 || mode == 3 {
			p.rows = make(map[int]*trackedLine)
		}
	case 'K':
		p.clearLine(mode)
	case 'h', 'l':
		if strings.Contains(params, "1049") {
			p.rows = make(map[int]*trackedLine)
			p.row, p.col = 0, 0
		}
	}
}

func parseCSIValues(params string) []int {
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		v, _ := strconv.Atoi(part)
		out = append(out, v)
	}
	return out
}

func (p *Parser) clearLine(mode int) {
	line := p.rows[p.row]
	switch mode {
	case 0:
		if line != nil && p.col < len(line.cells) {
			line.cells = line.cells[:p.col]
			line.active = couldBeApprovalLine(string(line.cells))
			line.invalidCandidate = false
		}
	case 1:
		delete(p.rows, p.row)
	default:
		delete(p.rows, p.row)
	}
}

func (p *Parser) snapshot() *Snapshot {
	promptCount := 0
	footerCount := 0
	var candidates []candidateLine
	for _, line := range p.rows {
		if line == nil {
			continue
		}
		if line.invalidCandidate {
			return nil
		}
		if line.prompt {
			promptCount++
		}
		if line.footer {
			footerCount++
		}
		if !line.active {
			continue
		}
		text := strings.TrimSpace(string(line.cells))
		if candidate, ok := parseCandidate(text); ok {
			candidates = append(candidates, candidate)
		}
	}
	if promptCount != 1 || footerCount != 1 || len(candidates) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(candidates))
	selected := 0
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.key < 1 || candidate.key > 9 || seen[candidate.key] {
			return nil
		}
		seen[candidate.key] = true
		if candidate.selected {
			if selected != 0 {
				return nil
			}
			selected = candidate.key
		}
		out = append(out, Candidate{Key: candidate.key, Decision: candidate.decision})
	}
	if selected == 0 || !seen[selected] {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	for i, candidate := range out {
		if candidate.Key != i+1 {
			return nil
		}
	}
	return &Snapshot{Candidates: out, SelectedKey: selected}
}

func parseCandidate(text string) (candidateLine, bool) {
	selected := false
	if strings.HasPrefix(text, "›") {
		selected = true
		text = strings.TrimSpace(strings.TrimPrefix(text, "›"))
	}
	if len(text) < 3 || text[0] < '1' || text[0] > '9' || text[1] != '.' {
		return candidateLine{}, false
	}
	key := int(text[0] - '0')
	label := strings.TrimSpace(text[2:])
	label = shortcutSuffix.ReplaceAllString(label, "")
	decision, ok := approvalLabels[label]
	if !ok {
		return candidateLine{}, false
	}
	return candidateLine{key: key, decision: decision, selected: selected}, true
}

func cloneSnapshot(in *Snapshot) *Snapshot {
	if in == nil {
		return nil
	}
	out := *in
	out.Candidates = append([]Candidate(nil), in.Candidates...)
	return &out
}
