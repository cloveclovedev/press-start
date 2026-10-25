// Package monitor keeps the failures of press-start that last and notifies an
// external monitor of the current state.
//
// The monitor has one check for the whole of press-start. A regular success
// would clear a failure that still lasts, so every notification sends the
// current state: a failure that lists the lasting kinds, or a success when no
// kind lasts.
package monitor

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mkuri/press-start/internal/core/settings"
)

// Notifier sends one notification to the external monitor.
type Notifier interface {
	Success(ctx context.Context) error
	Fail(ctx context.Context, body string) error
}

// Reporter takes the start and the end of a failure. Feature code reports
// through it, and a call never waits for the monitor.
type Reporter interface {
	// Fail records that a failure of the kind lasts.
	Fail(kind string)
	// Clear records that the failure of the kind ended.
	Clear(kind string)
}

// Clock tells the time and waits. Tests replace it so that they take no real
// time.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SettingsSource gives the current settings. *settings.Store is one.
type SettingsSource interface {
	Current() settings.Settings
}

// Monitor keeps the lasting failures and sends the notifications from Run.
type Monitor struct {
	clock    Clock
	settings SettingsSource
	notifier Notifier
	logger   *slog.Logger

	// changed wakes Run when a change waits to be sent. It holds at most
	// one signal, so Fail and Clear never block.
	changed chan struct{}

	mu      sync.Mutex
	kinds   map[string]bool
	pending bool
}

var _ Reporter = (*Monitor)(nil)

// New returns a Monitor with no lasting failure.
func New(clock Clock, source SettingsSource, notifier Notifier, logger *slog.Logger) *Monitor {
	return &Monitor{
		clock:    clock,
		settings: source,
		notifier: notifier,
		logger:   logger,
		changed:  make(chan struct{}, 1),
		kinds:    map[string]bool{},
	}
}

// Fail records that a failure of the kind lasts. A kind that starts asks Run
// to send a failure. Fail only changes the state and returns at once.
func (m *Monitor) Fail(kind string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.kinds[kind] {
		return
	}
	m.kinds[kind] = true
	m.markPending()
}

// Clear records that the failure of the kind ended. When the last kind
// ends, Clear asks Run to send a success. Clear only changes the state and
// returns at once.
func (m *Monitor) Clear(kind string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.kinds[kind] {
		return
	}
	delete(m.kinds, kind)
	if len(m.kinds) == 0 {
		m.markPending()
	}
}

// markPending must be called with mu held.
func (m *Monitor) markPending() {
	m.pending = true
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

// Run sends the current state right after it starts, then after each
// interval of the settings. It sends a change that Fail or Clear asks for
// at once, but never sooner than settings.MinMonitorGap after the last
// notification, so that the monitor takes every notification. It reads the
// settings at each wait, so a changed interval is used without a restart.
// Run returns ctx.Err().
func (m *Monitor) Run(ctx context.Context) error {
	last := m.clock.Now()
	m.send(ctx)
	for {
		m.mu.Lock()
		pending := m.pending
		m.mu.Unlock()

		next := last.Add(m.settings.Current().Monitor.Interval)
		if pending {
			next = last.Add(settings.MinMonitorGap)
		}
		now := m.clock.Now()
		if !now.Before(next) {
			m.send(ctx)
			last = now
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.changed:
		case <-m.clock.After(next.Sub(now)):
		}
	}
}

// send sends the current state. A notifier error is recorded, and the next
// notification goes out at its planned time.
func (m *Monitor) send(ctx context.Context) {
	m.mu.Lock()
	kinds := make([]string, 0, len(m.kinds))
	for k := range m.kinds {
		kinds = append(kinds, k)
	}
	// This notification sends every change so far, so a signal of those
	// changes must not wake Run again.
	m.pending = false
	select {
	case <-m.changed:
	default:
	}
	m.mu.Unlock()

	var err error
	if len(kinds) == 0 {
		err = m.notifier.Success(ctx)
	} else {
		slices.Sort(kinds)
		err = m.notifier.Fail(ctx, strings.Join(kinds, "\n"))
	}
	if err != nil {
		m.record(ctx, slog.LevelError, "monitor notify failed", "error", err)
	}
}

// record logs msg at the time of the Clock.
func (m *Monitor) record(ctx context.Context, level slog.Level, msg string, args ...any) {
	h := m.logger.Handler()
	if !h.Enabled(ctx, level) {
		return
	}
	r := slog.NewRecord(m.clock.Now(), level, msg, 0)
	r.Add(args...)
	_ = h.Handle(ctx, r)
}
