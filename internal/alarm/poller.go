package alarm

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mkuri/press-start/internal/core/calendar"
)

// Poller reads the calendar in its own goroutine and keeps the last events
// that it read. The Scheduler takes the events from the Poller, so the
// decision of the wake time never waits for the calendar.
type Poller struct {
	Clock    Clock
	Settings SettingsSource
	Source   calendar.Source
	Logger   *slog.Logger
	// Location is the time zone of the wake time.
	Location *time.Location

	mu     sync.Mutex
	events []calendar.Event
}

// Events returns the events of the last successful read. It is nil before
// the first successful read.
func (p *Poller) Events() []calendar.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.events)
}

// Run reads the calendar right away, then again after each read interval of
// the settings, until ctx ends. Each read asks for the events of today and
// tomorrow. A failed read keeps the last events and is recorded. Every
// failure waits the normal interval, also an expired token, because a quick
// retry does not fix it. A read that takes longer than the interval is
// stopped, so one stuck read does not stop the reads after it. Run returns
// ctx.Err().
func (p *Poller) Run(ctx context.Context) error {
	for {
		c := p.Settings.Current().Calendar
		p.read(ctx, c.Calendars, c.ReadInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.Clock.After(c.ReadInterval):
		}
	}
}

func (p *Poller) read(ctx context.Context, calendars []string, timeout time.Duration) {
	now := p.Clock.Now().In(p.Location)
	y, m, d := now.Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, p.Location)
	to := time.Date(y, m, d+2, 0, 0, 0, 0, p.Location)

	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	events, err := p.Source.Events(readCtx, calendars, from, to)
	if err != nil {
		if ctx.Err() != nil {
			// The service stops, so the read did not fail.
			return
		}
		p.record(ctx, slog.LevelError, "calendar read failed", "error", err, "token_expired", errors.Is(err, calendar.ErrTokenExpired))
		return
	}
	p.mu.Lock()
	p.events = events
	p.mu.Unlock()
}

// record logs msg at the time of the Clock.
func (p *Poller) record(ctx context.Context, level slog.Level, msg string, args ...any) {
	h := p.Logger.Handler()
	if !h.Enabled(ctx, level) {
		return
	}
	r := slog.NewRecord(p.Clock.Now(), level, msg, 0)
	r.Add(args...)
	_ = h.Handle(ctx, r)
}
