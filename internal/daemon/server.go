package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wakadorimk2/enterlight/internal/chroma"
)

var validStates = map[string]bool{
	"working": true,
	"waiting": true,
	"done":    true,
	"error":   true,
	"off":     true,
}

const chromaRetryDelay = 16 * time.Second

type chromaClient interface {
	Connected() bool
	SetEnter(context.Context, uint32) error
	Heartbeat(context.Context) error
	Close(context.Context) error
}

type Server struct {
	version string
	chroma  chromaClient

	mu            sync.RWMutex
	state         string
	since         time.Time
	lastError     string
	lastErrorCode string
	lastRGB       uint32
	lit           bool
	nextRetry     time.Time

	stateCh chan string
	stopCh  chan struct{}
}

func NewServer(version string) *Server {
	return &Server{
		version: version,
		chroma:  chroma.New(),
		state:   "off",
		since:   time.Now(),
		stateCh: make(chan string, 8),
		stopCh:  make(chan struct{}),
	}
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", Address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", Address, err)
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /set", s.handleSet)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("POST /shutdown", s.handleShutdown)

	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	go s.effectLoop(ctx)

	errCh := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
	case <-sigCh:
	case <-s.stopCh:
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	_ = s.chroma.Close(shutdownCtx)
	return nil
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	var req SetRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	state := strings.ToLower(strings.TrimSpace(req.State))
	if !validStates[state] {
		http.Error(w, "unknown state", http.StatusBadRequest)
		return
	}
	select {
	case s.stateCh <- state:
	default:
		<-s.stateCh
		s.stateCh <- state
	}
	writeJSON(w, map[string]any{"ok": true, "state": state})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	status := Status{
		State:         s.state,
		Since:         s.since,
		LastError:     s.lastError,
		LastErrorCode: s.lastErrorCode,
		Version:       s.version,
	}
	s.mu.RUnlock()
	status.ChromaConnected = s.chroma.Connected()
	writeJSON(w, status)
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) effectLoop(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	heartbeat := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()

	current := "off"
	since := time.Now()
	s.applyOff()

	for {
		select {
		case <-ctx.Done():
			return
		case state := <-s.stateCh:
			current = state
			since = time.Now()
			s.mu.Lock()
			s.state = state
			s.since = since
			s.mu.Unlock()
			if state == "off" {
				s.applyOff()
			} else {
				s.render(current, since, time.Now(), true)
			}
		case now := <-ticker.C:
			s.render(current, since, now, false)
			if current == "done" && now.Sub(since) >= 2400*time.Millisecond {
				current = "off"
				since = now
				s.mu.Lock()
				s.state = current
				s.since = since
				s.mu.Unlock()
				s.applyOff()
			}
		case <-heartbeat.C:
			if current != "off" && s.chroma.Connected() {
				hbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err := s.chroma.Heartbeat(hbCtx)
				cancel()
				if err != nil {
					s.setError(err)
				}
			}
		}
	}
}

func (s *Server) render(state string, since, now time.Time, force bool) {
	s.mu.RLock()
	nextRetry := s.nextRetry
	s.mu.RUnlock()
	if now.Before(nextRetry) {
		return
	}

	elapsed := now.Sub(since)
	var rgb uint32
	var on bool

	switch state {
	case "working":
		rgb, on = 0x3B82F6, true
	case "waiting":
		// Warm amber breathing between 25% and 100% brightness.
		phase := float64(elapsed%(1600*time.Millisecond)) / float64(1600*time.Millisecond)
		brightness := 0.25 + 0.75*(0.5-0.5*math.Cos(phase*2*math.Pi))
		rgb, on = scaleRGB(0xF59E0B, brightness), true
	case "done":
		// Three green pulses, then release the Chroma session.
		phase := elapsed % (800 * time.Millisecond)
		on = phase < 360*time.Millisecond && elapsed < 2400*time.Millisecond
		rgb = 0x22C55E
	case "error":
		on = (elapsed % (800 * time.Millisecond)) < 400*time.Millisecond
		rgb = 0xEF4444
	case "off":
		return
	default:
		return
	}

	s.mu.RLock()
	unchanged := s.lit == on && (!on || s.lastRGB == rgb)
	s.mu.RUnlock()
	if unchanged && !force {
		return
	}

	if !on {
		// Keep the session alive during blink gaps, but render black.
		rgb = 0x000000
	}
	setCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := s.chroma.SetEnter(setCtx, rgb)
	cancel()
	if err != nil {
		s.setErrorAt(err, now)
		return
	}

	s.mu.Lock()
	s.lastRGB = rgb
	s.lit = on
	s.lastError = ""
	s.lastErrorCode = ""
	s.nextRetry = time.Time{}
	s.mu.Unlock()
}

func (s *Server) applyOff() {
	wasConnected := s.chroma.Connected()
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err := s.chroma.Close(closeCtx)
	cancel()
	s.mu.Lock()
	s.lit = false
	s.lastRGB = 0
	if err != nil {
		s.lastError = err.Error()
		s.lastErrorCode = chroma.ErrorCode(err)
		s.nextRetry = time.Now().Add(chromaRetryDelay)
	} else if wasConnected {
		s.lastError = ""
		s.lastErrorCode = ""
		s.nextRetry = time.Time{}
	}
	s.mu.Unlock()
}

func (s *Server) setError(err error) {
	s.setErrorAt(err, time.Now())
}

func (s *Server) setErrorAt(err error, now time.Time) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.lastErrorCode = chroma.ErrorCode(err)
	s.nextRetry = now.Add(chromaRetryDelay)
	s.mu.Unlock()
}

func scaleRGB(rgb uint32, factor float64) uint32 {
	if factor < 0 {
		factor = 0
	}
	if factor > 1 {
		factor = 1
	}
	r := uint32(float64((rgb>>16)&0xff) * factor)
	g := uint32(float64((rgb>>8)&0xff) * factor)
	b := uint32(float64(rgb&0xff) * factor)
	return (r << 16) | (g << 8) | b
}
