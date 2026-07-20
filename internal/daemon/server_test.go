package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wakadorimk2/enterlight/internal/chroma"
)

type failingChroma struct {
	setCalls int
	err      error
}

func (f *failingChroma) Connected() bool { return false }
func (f *failingChroma) SetKeyboard(context.Context, chroma.Frame) error {
	f.setCalls++
	return f.err
}
func (f *failingChroma) Heartbeat(context.Context) error { return nil }
func (f *failingChroma) Close(context.Context) error     { return nil }

func TestRenderWaitsForSDKTimeoutBeforeCreatingAnotherSession(t *testing.T) {
	fake := &failingChroma{err: &chroma.Error{Code: chroma.ErrorSessionUnreachable, Err: errors.New("unreachable")}}
	s := NewServer("test")
	s.chroma = fake
	now := time.Now()
	if err := s.updateSession(SetRequest{SessionID: "test", State: "working"}, now); err != nil {
		t.Fatal(err)
	}
	s.renderAll(now, true)
	s.renderAll(now.Add(15*time.Second), true)
	if fake.setCalls != 1 {
		t.Fatalf("SetEnter called %d times before retry delay, want 1", fake.setCalls)
	}
	s.renderAll(now.Add(chromaRetryDelay), true)
	if fake.setCalls != 2 {
		t.Fatalf("SetEnter called %d times after retry delay, want 2", fake.setCalls)
	}
	if s.lastErrorCode != chroma.ErrorSessionUnreachable {
		t.Fatalf("lastErrorCode = %q", s.lastErrorCode)
	}
}

func TestOffPreservesFailedSessionBackoff(t *testing.T) {
	fake := &failingChroma{err: &chroma.Error{Code: chroma.ErrorSessionUnreachable, Err: errors.New("unreachable")}}
	s := NewServer("test")
	s.chroma = fake
	now := time.Now()
	if err := s.updateSession(SetRequest{SessionID: "test", State: "working"}, now); err != nil {
		t.Fatal(err)
	}
	s.renderAll(now, true)
	retry := s.nextRetry
	s.applyOff()
	if !s.nextRetry.Equal(retry) {
		t.Fatalf("off changed retry from %v to %v", retry, s.nextRetry)
	}
	if s.lastErrorCode != chroma.ErrorSessionUnreachable {
		t.Fatalf("off cleared lastErrorCode: %q", s.lastErrorCode)
	}
}

func TestPresetSpecifications(t *testing.T) {
	tests := []struct {
		name       string
		brightness float64
		period     time.Duration
		interval   time.Duration
	}{
		{"calm", .35, 3200 * time.Millisecond, 100 * time.Millisecond},
		{"vivid", .70, 2 * time.Second, 50 * time.Millisecond},
		{"max", 1, 1200 * time.Millisecond, 33 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := presetFor(tt.name)
			if got.Brightness != tt.brightness || got.MotionPeriod != tt.period || got.FrameInterval != tt.interval {
				t.Fatalf("presetFor(%q) = %+v", tt.name, got)
			}
		})
	}
}

func TestComposeFrameSplitsFourSessionsAcrossEveryRow(t *testing.T) {
	now := time.Unix(100, 0)
	states := []string{"working", "waiting", "error", "idle"}
	var sessions []sessionState
	for i, state := range states {
		sessions = append(sessions, sessionState{ID: state, State: state, Since: now.Add(-time.Second), CreatedAt: now.Add(time.Duration(i) * time.Millisecond), Visible: true})
	}
	frame := composeFrame(sessions, nil, presetFor("max"), now)
	for row := 0; row < chroma.Rows; row++ {
		nonzero := false
		for col := 0; col < chroma.Columns; col++ {
			nonzero = nonzero || frame.Colors[row][col] != 0
		}
		if !nonzero {
			t.Fatalf("row %d was not assigned to a lane", row)
		}
	}
}

func TestVisibleSessionsKeepsFourMostRecentlyUpdated(t *testing.T) {
	now := time.Unix(100, 0)
	var sessions []sessionState
	for i := 0; i < 5; i++ {
		sessions = append(sessions, sessionState{
			ID:        string(rune('a' + i)),
			State:     "working",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	got := visibleSessions(sessions)
	if len(got) != 4 {
		t.Fatalf("visible count = %d, want 4", len(got))
	}
	for _, session := range got {
		if session.ID == "a" {
			t.Fatal("oldest session should be retained but not displayed")
		}
	}
}

func TestIdleRequiresVisibility(t *testing.T) {
	now := time.Now()
	hidden := sessionState{ID: "hidden", State: "idle", CreatedAt: now}
	shown := sessionState{ID: "shown", State: "idle", CreatedAt: now.Add(time.Second), Visible: true}
	got := visibleSessions([]sessionState{hidden, shown})
	if len(got) != 1 || got[0].ID != "shown" {
		t.Fatalf("visible sessions = %+v", got)
	}
}

func TestApprovalOverlayUsesDecisionKeysAndSelectedColorForEnter(t *testing.T) {
	now := time.Now()
	approval, err := validatedApproval(ApprovalRequest{
		SessionID: "s1",
		Candidates: []ApprovalCandidate{
			{Key: 1, Decision: "allow_once"},
			{Key: 2, Decision: "allow_session"},
			{Key: 3, Decision: "decline"},
			{Key: 4, Decision: "cancel"},
		},
		SelectedKey: 2,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	frame := composeFrame(nil, approval, presetFor("calm"), now)
	for key, decision := range []string{"allow_once", "allow_session", "decline", "cancel"} {
		if got := frame.Keys[1][key+2]; got != decisionColors[decision] {
			t.Fatalf("number key %d = %#x, want %#x", key+1, got, decisionColors[decision])
		}
	}
	if got := frame.Keys[chroma.EnterRow][chroma.EnterCol]; got != decisionColors["allow_session"] {
		t.Fatalf("Enter = %#x, want selected color %#x", got, decisionColors["allow_session"])
	}
}

func TestApprovalValidationFailsClosed(t *testing.T) {
	tests := []ApprovalRequest{
		{SessionID: "s", Candidates: []ApprovalCandidate{{Key: 1, Decision: "allow_once"}}, SelectedKey: 2},
		{SessionID: "s", Candidates: []ApprovalCandidate{{Key: 1, Decision: "unknown"}}, SelectedKey: 1},
		{SessionID: "s", Candidates: []ApprovalCandidate{{Key: 1, Decision: "cancel"}, {Key: 1, Decision: "decline"}}, SelectedKey: 1},
		{SessionID: "s", Candidates: []ApprovalCandidate{{Key: 1, Decision: "cancel"}}, SelectedKey: 1, LeaseMS: 20_000},
	}
	for i, request := range tests {
		if _, err := validatedApproval(request, time.Now()); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestInvalidApprovalRequestClearsMatchingOverlay(t *testing.T) {
	now := time.Now()
	s := NewServer("test")
	s.approval = &approvalState{
		SessionID:   "s1",
		Candidates:  []ApprovalCandidate{{Key: 1, Decision: "allow_once"}},
		SelectedKey: 1,
		ExpiresAt:   now.Add(time.Minute),
	}
	req := httptest.NewRequest(http.MethodPost, "/approval", strings.NewReader(`{
		"sessionId":"s1",
		"candidates":[{"key":1,"decision":"allow_once"}],
		"selectedKey":2
	}`))
	response := httptest.NewRecorder()
	s.handleApproval(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if s.approval != nil {
		t.Fatal("invalid update left the approval overlay active")
	}
}

func TestLeavingWaitingClearsApprovalOverlay(t *testing.T) {
	now := time.Now()
	s := NewServer("test")
	if err := s.updateSession(SetRequest{SessionID: "s1", State: "waiting"}, now); err != nil {
		t.Fatal(err)
	}
	s.approval = &approvalState{
		SessionID:   "s1",
		Candidates:  []ApprovalCandidate{{Key: 1, Decision: "allow_once"}},
		SelectedKey: 1,
		ExpiresAt:   now.Add(time.Minute),
	}
	if err := s.updateSession(SetRequest{SessionID: "s1", State: "working"}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if s.approval != nil {
		t.Fatal("non-waiting state left the approval overlay active")
	}
}

func TestApprovalLeaseAndDoneSessionExpire(t *testing.T) {
	now := time.Now()
	s := NewServer("test")
	s.sessions["done"] = &sessionState{ID: "done", State: "done", Since: now, UpdatedAt: now, CreatedAt: now}
	s.approval = &approvalState{SessionID: "done", Candidates: []ApprovalCandidate{{Key: 1, Decision: "allow_once"}}, SelectedKey: 1, ExpiresAt: now.Add(time.Second)}
	sessions, approval := s.snapshot(now.Add(doneDuration + time.Millisecond))
	if len(sessions) != 0 || approval != nil {
		t.Fatalf("expired state remained: sessions=%+v approval=%+v", sessions, approval)
	}
	if s.state != "off" {
		t.Fatalf("state = %q, want off", s.state)
	}
}

func TestConcurrentApprovalAdaptersFailClosed(t *testing.T) {
	s := NewServer("test")
	body := func(session string) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/approval", strings.NewReader(`{"sessionId":"`+session+`","candidates":[{"key":1,"decision":"allow_once"}],"selectedKey":1,"leaseMs":5000}`))
	}
	first := httptest.NewRecorder()
	s.handleApproval(first, body("adapter-a"))
	if first.Code != http.StatusOK || s.approval == nil {
		t.Fatalf("first update: status=%d approval=%#v", first.Code, s.approval)
	}

	second := httptest.NewRecorder()
	s.handleApproval(second, body("adapter-b"))
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d, want 409", second.Code)
	}
	if s.approval != nil {
		t.Fatal("conflicting adapters left an overlay active")
	}

	// Neither contender may win merely by renewing while both leases live.
	renew := httptest.NewRecorder()
	s.handleApproval(renew, body("adapter-a"))
	if renew.Code != http.StatusConflict || s.approval != nil {
		t.Fatalf("renew status=%d approval=%#v", renew.Code, s.approval)
	}

	s.clearApproval("adapter-b")
	renew = httptest.NewRecorder()
	s.handleApproval(renew, body("adapter-a"))
	if renew.Code != http.StatusOK || s.approval == nil {
		t.Fatalf("post-clear renew status=%d approval=%#v", renew.Code, s.approval)
	}
}
