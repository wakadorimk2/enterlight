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
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wakadorimk2/enterlight/internal/chroma"
	enterconfig "github.com/wakadorimk2/enterlight/internal/config"
)

var validStates = map[string]bool{
	"working": true,
	"waiting": true,
	"done":    true,
	"error":   true,
	"idle":    true,
	"off":     true,
}

var decisionColors = map[string]uint32{
	"allow_once":    0x3B82F6,
	"allow_session": 0x22C55E,
	"decline":       0xF59E0B,
	"cancel":        0xEF4444,
}

const (
	chromaRetryDelay     = 16 * time.Second
	doneDuration         = 2400 * time.Millisecond
	defaultApprovalLease = 5 * time.Second
	minApprovalLease     = 500 * time.Millisecond
	maxApprovalLease     = 10 * time.Second
)

type chromaClient interface {
	Connected() bool
	SetKeyboard(context.Context, chroma.Frame) error
	Heartbeat(context.Context) error
	Close(context.Context) error
}

type sessionState struct {
	ID        string
	State     string
	Since     time.Time
	UpdatedAt time.Time
	CreatedAt time.Time
	Visible   bool
}

type approvalState struct {
	SessionID   string
	Candidates  []ApprovalCandidate
	SelectedKey int
	ExpiresAt   time.Time
}

type Server struct {
	version string
	chroma  chromaClient

	mu            sync.RWMutex
	state         string
	since         time.Time
	sessions      map[string]*sessionState
	approval      *approvalState
	preset        string
	configError   string
	lastError     string
	lastErrorCode string
	nextRetry     time.Time
	lastFrame     chroma.Frame
	hasFrame      bool
	lastRender    time.Time

	wakeCh chan struct{}
	stopCh chan struct{}
}

func NewServer(version string) *Server {
	cfg, cfgErr := enterconfig.Load()
	configError := ""
	if cfgErr != nil {
		configError = cfgErr.Error()
	}
	return &Server{
		version:     version,
		chroma:      chroma.New(),
		state:       "off",
		since:       time.Now(),
		sessions:    make(map[string]*sessionState),
		preset:      cfg.Preset,
		configError: configError,
		wakeCh:      make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
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
	mux.HandleFunc("POST /sessions/update", s.handleSet)
	mux.HandleFunc("POST /sessions/visibility", s.handleVisibility)
	mux.HandleFunc("POST /approval", s.handleApproval)
	mux.HandleFunc("DELETE /approval", s.handleApprovalClear)
	mux.HandleFunc("POST /preset", s.handlePreset)
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
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if err := s.updateSession(req, time.Now()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "sessionId": normalizedSessionID(req.SessionID), "state": strings.ToLower(strings.TrimSpace(req.State))})
}

func (s *Server) handleVisibility(w http.ResponseWriter, r *http.Request) {
	var req VisibilityRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	id, err := validateSessionID(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	s.mu.Lock()
	session, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	session.Visible = req.Visible
	session.UpdatedAt = now
	s.mu.Unlock()
	s.wake()
	writeJSON(w, map[string]any{"ok": true, "sessionId": id, "visible": req.Visible})
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	var req ApprovalRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	approval, err := validatedApproval(req, time.Now())
	if err != nil {
		s.clearApproval(strings.TrimSpace(req.SessionID))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.approval = approval
	s.mu.Unlock()
	s.wake()
	writeJSON(w, map[string]any{"ok": true, "sessionId": approval.SessionID, "expiresAt": approval.ExpiresAt})
}

func (s *Server) handleApprovalClear(w http.ResponseWriter, r *http.Request) {
	var req ApprovalClearRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	id, err := validateSessionID(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.clearApproval(id)
	writeJSON(w, map[string]any{"ok": true, "sessionId": id})
}

func (s *Server) handlePreset(w http.ResponseWriter, r *http.Request) {
	var req PresetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	if !enterconfig.ValidPreset(req.Preset) {
		http.Error(w, "unknown preset", http.StatusBadRequest)
		return
	}
	if _, err := enterconfig.Save(enterconfig.Config{Preset: req.Preset}); err != nil {
		http.Error(w, "save preset: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.preset = req.Preset
	s.configError = ""
	s.mu.Unlock()
	s.wake()
	writeJSON(w, map[string]any{"ok": true, "preset": req.Preset})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	now := time.Now()
	sessions, approval := s.snapshot(now)
	displayed := map[string]bool{}
	for _, session := range visibleSessions(sessions) {
		displayed[session.ID] = true
	}
	s.mu.RLock()
	status := Status{
		State:         s.state,
		Since:         s.since,
		LastError:     s.lastError,
		LastErrorCode: s.lastErrorCode,
		Version:       s.version,
		Preset:        s.preset,
		ConfigError:   s.configError,
	}
	s.mu.RUnlock()
	for _, session := range sessions {
		status.Sessions = append(status.Sessions, SessionStatus{
			SessionID: session.ID,
			State:     session.State,
			Since:     session.Since,
			UpdatedAt: session.UpdatedAt,
			Visible:   session.Visible,
			Displayed: displayed[session.ID],
		})
	}
	if approval != nil {
		status.Approval = &ApprovalStatus{
			SessionID:   approval.SessionID,
			Candidates:  append([]ApprovalCandidate(nil), approval.Candidates...),
			SelectedKey: approval.SelectedKey,
			ExpiresAt:   approval.ExpiresAt,
		}
	}
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

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := dec.Decode(out); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) updateSession(req SetRequest, now time.Time) error {
	id, err := validateSessionID(req.SessionID)
	if err != nil {
		return err
	}
	state := strings.ToLower(strings.TrimSpace(req.State))
	if !validStates[state] {
		return errors.New("unknown state")
	}
	s.mu.Lock()
	s.state = state
	s.since = now
	if state == "off" {
		delete(s.sessions, id)
		if s.approval != nil && s.approval.SessionID == id {
			s.approval = nil
		}
		s.refreshPrimaryStateLocked(now)
	} else if existing, ok := s.sessions[id]; ok {
		existing.State = state
		existing.Since = now
		existing.UpdatedAt = now
		if req.Visible != nil {
			existing.Visible = *req.Visible
		}
	} else {
		visible := false
		if req.Visible != nil {
			visible = *req.Visible
		}
		s.sessions[id] = &sessionState{ID: id, State: state, Since: now, UpdatedAt: now, CreatedAt: now, Visible: visible}
	}
	if state != "waiting" && s.approval != nil && s.approval.SessionID == id {
		s.approval = nil
	}
	s.mu.Unlock()
	s.wake()
	return nil
}

func normalizedSessionID(value string) string {
	if strings.TrimSpace(value) == "" {
		return "manual"
	}
	return strings.TrimSpace(value)
}

func validateSessionID(value string) (string, error) {
	id := normalizedSessionID(value)
	if len(id) > 128 {
		return "", errors.New("sessionId is too long")
	}
	if strings.ContainsAny(id, "\r\n\x00") {
		return "", errors.New("invalid sessionId")
	}
	return id, nil
}

func validatedApproval(req ApprovalRequest, now time.Time) (*approvalState, error) {
	id, err := validateSessionID(req.SessionID)
	if err != nil {
		return nil, err
	}
	if len(req.Candidates) == 0 || len(req.Candidates) > 9 {
		return nil, errors.New("candidates must contain 1 to 9 decisions")
	}
	seen := map[int]bool{}
	selected := false
	for _, candidate := range req.Candidates {
		if candidate.Key < 1 || candidate.Key > 9 || seen[candidate.Key] {
			return nil, errors.New("candidate keys must be unique numbers from 1 to 9")
		}
		if _, ok := decisionColors[candidate.Decision]; !ok {
			return nil, fmt.Errorf("unknown decision %q", candidate.Decision)
		}
		seen[candidate.Key] = true
		selected = selected || candidate.Key == req.SelectedKey
	}
	if !selected {
		return nil, errors.New("selectedKey must identify a candidate")
	}
	lease := defaultApprovalLease
	if req.LeaseMS != 0 {
		lease = time.Duration(req.LeaseMS) * time.Millisecond
		if lease < minApprovalLease || lease > maxApprovalLease {
			return nil, errors.New("leaseMs must be between 500 and 10000")
		}
	}
	return &approvalState{
		SessionID:   id,
		Candidates:  append([]ApprovalCandidate(nil), req.Candidates...),
		SelectedKey: req.SelectedKey,
		ExpiresAt:   now.Add(lease),
	}, nil
}

func (s *Server) clearApproval(sessionID string) {
	s.mu.Lock()
	if s.approval != nil && (sessionID == "" || s.approval.SessionID == sessionID) {
		s.approval = nil
	}
	s.mu.Unlock()
	s.wake()
}

func (s *Server) wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *Server) effectLoop(ctx context.Context) {
	ticker := time.NewTicker(33 * time.Millisecond)
	heartbeat := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	s.applyOff()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wakeCh:
			s.renderAll(time.Now(), true)
		case now := <-ticker.C:
			s.renderAll(now, false)
		case <-heartbeat.C:
			if s.chroma.Connected() {
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

func (s *Server) renderAll(now time.Time, force bool) {
	sessions, approval := s.snapshot(now)
	preset := s.currentPreset()
	if !force && now.Sub(s.lastRenderTime()) < preset.FrameInterval {
		return
	}
	visible := visibleSessions(sessions)
	if len(visible) == 0 && approval == nil {
		s.applyOff()
		return
	}
	s.mu.RLock()
	nextRetry := s.nextRetry
	s.mu.RUnlock()
	if now.Before(nextRetry) {
		return
	}
	frame := composeFrame(visible, approval, preset, now)
	s.mu.RLock()
	unchanged := s.hasFrame && frame == s.lastFrame
	s.mu.RUnlock()
	if unchanged && !force {
		return
	}
	setCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := s.chroma.SetKeyboard(setCtx, frame)
	cancel()
	if err != nil {
		s.setErrorAt(err, now)
		return
	}
	s.mu.Lock()
	s.lastFrame = frame
	s.hasFrame = true
	s.lastRender = now
	s.lastError = ""
	s.lastErrorCode = ""
	s.nextRetry = time.Time{}
	s.mu.Unlock()
}

func (s *Server) snapshot(now time.Time) ([]sessionState, *approvalState) {
	s.mu.Lock()
	removed := false
	for id, session := range s.sessions {
		if session.State == "done" && now.Sub(session.Since) >= doneDuration {
			delete(s.sessions, id)
			if s.approval != nil && s.approval.SessionID == id {
				s.approval = nil
			}
			removed = true
		}
	}
	if removed {
		s.refreshPrimaryStateLocked(now)
	}
	if s.approval != nil && !now.Before(s.approval.ExpiresAt) {
		s.approval = nil
	}
	sessions := make([]sessionState, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, *session)
	}
	var approval *approvalState
	if s.approval != nil {
		copyValue := *s.approval
		copyValue.Candidates = append([]ApprovalCandidate(nil), s.approval.Candidates...)
		approval = &copyValue
	}
	s.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].CreatedAt.Before(sessions[j].CreatedAt) })
	return sessions, approval
}

func (s *Server) refreshPrimaryStateLocked(now time.Time) {
	if len(s.sessions) == 0 {
		s.state = "off"
		s.since = now
		return
	}
	var latest *sessionState
	for _, session := range s.sessions {
		if latest == nil || session.UpdatedAt.After(latest.UpdatedAt) {
			latest = session
		}
	}
	s.state = latest.State
	s.since = latest.Since
}

func visibleSessions(all []sessionState) []sessionState {
	eligible := make([]sessionState, 0, len(all))
	for _, session := range all {
		if session.State != "idle" || session.Visible {
			eligible = append(eligible, session)
		}
	}
	if len(eligible) <= 4 {
		return eligible
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].UpdatedAt.After(eligible[j].UpdatedAt) })
	eligible = eligible[:4]
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].CreatedAt.Before(eligible[j].CreatedAt) })
	return eligible
}

type presetSpec struct {
	Name          string
	Brightness    float64
	MotionPeriod  time.Duration
	FrameInterval time.Duration
}

func presetFor(name string) presetSpec {
	switch name {
	case "vivid":
		return presetSpec{Name: name, Brightness: .70, MotionPeriod: 2 * time.Second, FrameInterval: 50 * time.Millisecond}
	case "max":
		return presetSpec{Name: name, Brightness: 1, MotionPeriod: 1200 * time.Millisecond, FrameInterval: 33 * time.Millisecond}
	default:
		return presetSpec{Name: "calm", Brightness: .35, MotionPeriod: 3200 * time.Millisecond, FrameInterval: 100 * time.Millisecond}
	}
}

func (s *Server) currentPreset() presetSpec {
	s.mu.RLock()
	name := s.preset
	s.mu.RUnlock()
	return presetFor(name)
}

func (s *Server) lastRenderTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRender
}

func composeFrame(sessions []sessionState, approval *approvalState, preset presetSpec, now time.Time) chroma.Frame {
	var frame chroma.Frame
	start := 0
	for index, session := range sessions {
		remainingRows := chroma.Rows - start
		remainingLanes := len(sessions) - index
		rows := remainingRows / remainingLanes
		if remainingRows%remainingLanes != 0 {
			rows++
		}
		end := start + rows
		renderScene(&frame, session, preset, now, start, end)
		start = end
	}
	if approval != nil {
		selectedColor := uint32(0)
		for _, candidate := range approval.Candidates {
			color := decisionColors[candidate.Decision]
			frame.Keys[1][candidate.Key+1] = color
			if candidate.Key == approval.SelectedKey {
				selectedColor = color
			}
		}
		frame.Keys[chroma.EnterRow][chroma.EnterCol] = selectedColor
	}
	return frame
}

func renderScene(frame *chroma.Frame, session sessionState, preset presetSpec, now time.Time, startRow, endRow int) {
	elapsed := now.Sub(session.Since)
	phase := float64(elapsed%preset.MotionPeriod) / float64(preset.MotionPeriod) * 2 * math.Pi
	for row := startRow; row < endRow; row++ {
		for col := 0; col < chroma.Columns; col++ {
			var color uint32
			switch session.State {
			case "working":
				mix := .5 + .5*math.Sin(float64(col)*.52+float64(row)*.29-phase)
				color = scaleRGB(lerpRGB(0x22D3EE, 0xA855F7, mix), preset.Brightness)
			case "waiting":
				breath := .25 + .75*(.5-.5*math.Cos(phase))
				color = scaleRGB(0xF59E0B, preset.Brightness*breath)
			case "done":
				progress := math.Min(1, float64(elapsed)/float64(doneDuration))
				burst := math.Max(0, math.Sin(progress*3*math.Pi))
				distance := math.Abs(float64(col)-10.5)/10.5 + math.Abs(float64(row)-2.5)/3
				intensity := math.Max(.12, burst*(1-math.Min(1, math.Abs(distance-progress*2))))
				color = scaleRGB(lerpRGB(0x22C55E, 0xFFFFFF, burst*.7), preset.Brightness*intensity)
			case "error":
				step := int(float64(elapsed) / float64(preset.MotionPeriod/4))
				if (row+col+step)%4 < 2 {
					color = scaleRGB(0xEF4444, preset.Brightness)
				} else {
					color = scaleRGB(0xF97316, preset.Brightness*.45)
				}
			case "idle":
				mix := .5 + .5*math.Sin(float64(col)*.28-phase*.45)
				color = scaleRGB(lerpRGB(0x0891B2, 0x7E22CE, mix), preset.Brightness*.22)
			}
			frame.Colors[row][col] = color
		}
	}
}

func lerpRGB(a, b uint32, amount float64) uint32 {
	if amount < 0 {
		amount = 0
	}
	if amount > 1 {
		amount = 1
	}
	ar, ag, ab := float64((a>>16)&0xff), float64((a>>8)&0xff), float64(a&0xff)
	br, bg, bb := float64((b>>16)&0xff), float64((b>>8)&0xff), float64(b&0xff)
	return uint32(ar+(br-ar)*amount)<<16 | uint32(ag+(bg-ag)*amount)<<8 | uint32(ab+(bb-ab)*amount)
}

func (s *Server) applyOff() {
	s.mu.RLock()
	shouldClose := s.hasFrame || s.chroma.Connected()
	s.mu.RUnlock()
	if !shouldClose {
		return
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err := s.chroma.Close(closeCtx)
	cancel()
	s.mu.Lock()
	s.hasFrame = false
	s.lastFrame = chroma.Frame{}
	if err != nil {
		s.lastError = err.Error()
		s.lastErrorCode = chroma.ErrorCode(err)
		s.nextRetry = time.Now().Add(chromaRetryDelay)
	} else {
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
