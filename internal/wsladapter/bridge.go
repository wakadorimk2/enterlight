package wsladapter

import (
	"context"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const (
	approvalLeaseMS = 5000
	refreshInterval = 2 * time.Second
)

type bridge struct {
	executable string
	sessionID  string
	updates    chan *Snapshot
	done       chan struct{}
	stopped    chan struct{}
	once       sync.Once
}

func newBridge(executable, sessionID string) *bridge {
	b := &bridge{executable: executable, sessionID: sessionID, updates: make(chan *Snapshot, 1), done: make(chan struct{}), stopped: make(chan struct{})}
	go b.run()
	return b
}

func (b *bridge) Update(snapshot *Snapshot) {
	snapshot = cloneSnapshot(snapshot)
	select {
	case b.updates <- snapshot:
	default:
		select {
		case <-b.updates:
		default:
		}
		select {
		case b.updates <- snapshot:
		default:
		}
	}
}

func (b *bridge) Close() {
	b.once.Do(func() { close(b.done) })
	<-b.stopped
}

func (b *bridge) run() {
	defer close(b.stopped)
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	var current *Snapshot
	for {
		select {
		case snapshot := <-b.updates:
			current = snapshot
			if current == nil {
				b.clear()
			} else {
				b.set(current)
			}
		case <-ticker.C:
			if current != nil {
				b.set(current)
			}
		case <-b.done:
			b.clear()
			return
		}
	}
}

func (b *bridge) set(snapshot *Snapshot) {
	args := []string{"approval", "set", "--session", b.sessionID, "--selected", strconv.Itoa(snapshot.SelectedKey), "--lease", strconv.Itoa(approvalLeaseMS)}
	for _, candidate := range snapshot.Candidates {
		args = append(args, "--candidate", strconv.Itoa(candidate.Key)+"="+candidate.Decision)
	}
	b.execute(args...)
}

func (b *bridge) clear() { b.execute("approval", "clear", "--session", b.sessionID) }

func (b *bridge) execute(args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.executable, args...)
	// Adapter failures must never corrupt the terminal or disclose its content.
	_ = cmd.Run()
}
