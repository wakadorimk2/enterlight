package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wakadorimk2/enterlight/internal/chroma"
)

type failingChroma struct {
	setCalls int
	err      error
}

func (f *failingChroma) Connected() bool                        { return false }
func (f *failingChroma) SetEnter(context.Context, uint32) error { f.setCalls++; return f.err }
func (f *failingChroma) Heartbeat(context.Context) error        { return nil }
func (f *failingChroma) Close(context.Context) error            { return nil }

func TestRenderWaitsForSDKTimeoutBeforeCreatingAnotherSession(t *testing.T) {
	fake := &failingChroma{err: &chroma.Error{Code: chroma.ErrorSessionUnreachable, Err: errors.New("unreachable")}}
	s := NewServer("test")
	s.chroma = fake
	now := time.Now()
	s.render("working", now, now, true)
	s.render("working", now, now.Add(15*time.Second), true)
	if fake.setCalls != 1 {
		t.Fatalf("SetEnter called %d times before retry delay, want 1", fake.setCalls)
	}
	s.render("working", now, now.Add(chromaRetryDelay), true)
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
	s.render("working", now, now, true)
	retry := s.nextRetry
	s.applyOff()
	if !s.nextRetry.Equal(retry) {
		t.Fatalf("off changed retry from %v to %v", retry, s.nextRetry)
	}
	if s.lastErrorCode != chroma.ErrorSessionUnreachable {
		t.Fatalf("off cleared lastErrorCode: %q", s.lastErrorCode)
	}
}
