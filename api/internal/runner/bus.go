package runner

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event is one line of a run's live log (RunEvent in
// server/src/vendor/shared/contracts/trace.ts).
type Event struct {
	RunID string `json:"runId"`
	Seq   int    `json:"seq"`  // from 1
	Kind  string `json:"kind"` // info, tool, result or error
	Msg   string `json:"msg"`
	T     string `json:"t"` // the server's time of day, HH:MM:SS
}

// Kinds of events.
const (
	kindInfo   = "info"
	kindTool   = "tool" // a call to something outside: git, a model
	kindResult = "result"
	kindError  = "error"
)

// keepFinished is how long a finished run's events stay in memory, for a
// page that opens its live log late. Its log is saved in its trace too.
const keepFinished = 10 * time.Minute

// bus keeps each run's events in memory and wakes whoever follows the run.
type bus struct {
	mu   sync.Mutex
	runs map[uuid.UUID]*runState
	now  func() time.Time
}

type runState struct {
	events    []Event
	done      bool
	doneAt    time.Time
	wake      chan struct{} // closed and replaced when an event arrives or the run ends
	cancel    context.CancelFunc
	cancelled bool // by the user
}

func newBus() *bus {
	return &bus{runs: map[uuid.UUID]*runState{}, now: time.Now}
}

// add registers a run; cancel stops its work.
func (b *bus) add(run uuid.UUID, cancel context.CancelFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, s := range b.runs {
		if s.done && b.now().Sub(s.doneAt) > keepFinished {
			delete(b.runs, id)
		}
	}
	b.runs[run] = &runState{wake: make(chan struct{}), cancel: cancel}
}

// publish adds an event to a run's log. A run the bus doesn't know gets
// none.
func (b *bus) publish(run uuid.UUID, kind, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.runs[run]
	if !ok {
		return
	}
	s.events = append(s.events, Event{RunID: run.String(), Seq: len(s.events) + 1, Kind: kind, Msg: msg, T: b.now().Format("15:04:05")})
	close(s.wake)
	s.wake = make(chan struct{})
}

// complete marks a run ended: its followers stop after its last event.
func (b *bus) complete(run uuid.UUID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.runs[run]
	if !ok || s.done {
		return
	}
	s.done, s.doneAt = true, b.now()
	s.cancel() // release its context
	close(s.wake)
	s.wake = make(chan struct{})
}

// cancelByUser stops a run's work, and remembers the user asked.
func (b *bus) cancelByUser(run uuid.UUID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.runs[run]; ok && !s.done {
		s.cancelled = true
		s.cancel()
	}
}

// cancelledByUser reports whether the user cancelled the run.
func (b *bus) cancelledByUser(run uuid.UUID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.runs[run]
	return ok && s.cancelled
}

// log returns a copy of a run's events so far.
func (b *bus) log(run uuid.UUID) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.runs[run]; ok {
		return append([]Event(nil), s.events...)
	}
	return nil
}

// follow calls send with each of a run's events, the earlier ones first,
// until the run ends, send fails or ctx is done. It returns false when the
// bus doesn't know the run: it ran on another server, or ended long ago.
func (b *bus) follow(ctx context.Context, run uuid.UUID, send func(Event) error) (bool, error) {
	b.mu.Lock()
	s, ok := b.runs[run]
	if !ok {
		b.mu.Unlock()
		return false, nil
	}
	for next := 0; ; {
		events := append([]Event(nil), s.events[next:]...)
		done, wake := s.done, s.wake
		b.mu.Unlock()

		for _, e := range events {
			if err := send(e); err != nil {
				return true, err
			}
		}
		next += len(events)
		if done {
			return true, nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return true, ctx.Err()
		}
		b.mu.Lock()
	}
}
