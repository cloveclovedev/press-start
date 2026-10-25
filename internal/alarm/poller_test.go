package alarm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/mkuri/press-start/internal/core/calendar"
	"github.com/mkuri/press-start/internal/core/settings"
)

var _ EventSource = (*Poller)(nil)

type sourceCall struct {
	at        time.Time
	calendars []string
	from, to  time.Time
}

type sourceResult struct {
	events []calendar.Event
	err    error
}

// fakeSource answers each read with the next result. After the last result,
// it repeats the last one.
type fakeSource struct {
	clock   *fakeClock
	results []sourceResult
	calls   []sourceCall
}

func (s *fakeSource) Events(_ context.Context, calendars []string, from, to time.Time) ([]calendar.Event, error) {
	s.calls = append(s.calls, sourceCall{at: s.clock.now, calendars: calendars, from: from, to: to})
	r := s.results[min(len(s.calls), len(s.results))-1]
	return r.events, r.err
}

// silentSource never answers. It returns only when the read is stopped.
type silentSource struct{}

func (silentSource) Events(ctx context.Context, _ []string, _, _ time.Time) ([]calendar.Event, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// stillClock never moves and never fires.
type stillClock struct{ now time.Time }

func (c stillClock) Now() time.Time                     { return c.now }
func (stillClock) After(time.Duration) <-chan time.Time { return nil }

type pollerLogLine struct {
	Level        string `json:"level"`
	Msg          string `json:"msg"`
	Error        string `json:"error"`
	TokenExpired bool   `json:"token_expired"`
}

// runPoller runs a Poller on a fake clock from start to end.
func runPoller(t *testing.T, start, end time.Time, current func(now time.Time) settings.Calendar, src *fakeSource) (*Poller, []pollerLogLine) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &fakeClock{now: start, end: end, cancel: cancel}
	src.clock = clock
	var log bytes.Buffer
	p := &Poller{
		Clock: clock,
		Settings: sourceFunc(func() settings.Settings {
			return settings.Settings{Calendar: current(clock.now)}
		}),
		Source:   src,
		Logger:   slog.New(slog.NewJSONHandler(&log, nil)),
		Location: tokyo,
	}
	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want %v", err, context.Canceled)
	}
	var lines []pollerLogLine
	sc := bufio.NewScanner(&log)
	for sc.Scan() {
		var l pollerLogLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}
		lines = append(lines, l)
	}
	return p, lines
}

func calendarEvery(interval time.Duration) func(time.Time) settings.Calendar {
	return func(time.Time) settings.Calendar {
		return settings.Calendar{LeadTime: 10 * time.Minute, ReadInterval: interval}
	}
}

func TestPollerKeepsLastEventsAfterFailure(t *testing.T) {
	event := []calendar.Event{{Start: at(14, 7, 0, 0)}}
	failed := sourceResult{err: errors.New("connection refused")}
	tests := []struct {
		name     string
		results  []sourceResult
		failures int
		wake     time.Time
	}{
		{"events read before", []sourceResult{{events: event}, failed}, 2, at(14, 6, 50, 0)},
		{"no events read", []sourceResult{failed}, 3, at(14, 7, 30, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reads at 05:00, 05:15, and 05:30.
			p, lines := runPoller(t, at(14, 5, 0, 0), at(14, 5, 40, 0), calendarEvery(15*time.Minute), &fakeSource{results: tt.results})
			var failures int
			for _, l := range lines {
				if l.Msg != "calendar read failed" {
					t.Errorf("unexpected log line %+v", l)
					continue
				}
				failures++
				if l.Level != "ERROR" || l.Error != "connection refused" || l.TokenExpired {
					t.Errorf("failure line = %+v, want ERROR with the error", l)
				}
			}
			if failures != tt.failures {
				t.Errorf("calendar read failed lines = %d, want %d", failures, tt.failures)
			}

			f := runSchedulerEvents(t, at(14, 6, 0, 0), at(14, 8, 0, 0), func(time.Time) settings.Settings {
				return settings.Settings{Alarm: alarmAt(7, 30), Calendar: settings.Calendar{LeadTime: 10 * time.Minute}}
			}, p, &fakePlayer{})
			plays := f.player.plays()
			if len(plays) != 1 {
				t.Fatalf("Play calls = %d, want 1: %+v", len(plays), plays)
			}
			checkStart(t, plays[0].at, tt.wake)
		})
	}
}

func TestPollerExpiredTokenWaitsNormalInterval(t *testing.T) {
	expired := sourceResult{err: fmt.Errorf("google: %w", calendar.ErrTokenExpired)}
	src := &fakeSource{results: []sourceResult{expired}}
	_, lines := runPoller(t, at(14, 5, 0, 0), at(14, 5, 40, 0), calendarEvery(15*time.Minute), src)
	var got []time.Time
	for _, c := range src.calls {
		got = append(got, c.at)
	}
	want := []time.Time{at(14, 5, 0, 0), at(14, 5, 15, 0), at(14, 5, 30, 0)}
	if !slices.EqualFunc(got, want, time.Time.Equal) {
		t.Errorf("reads at %v, want %v", got, want)
	}
	if len(lines) != 3 {
		t.Fatalf("log = %+v, want 3 lines", lines)
	}
	for _, l := range lines {
		if l.Msg != "calendar read failed" || !l.TokenExpired {
			t.Errorf("log line = %+v, want calendar read failed with token_expired", l)
		}
	}
}

func TestPollerNeverAnsweringSourceStillRings(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &Poller{
		Clock: stillClock{now: at(14, 6, 0, 0)},
		Settings: sourceFunc(func() settings.Settings {
			return settings.Settings{Calendar: settings.Calendar{ReadInterval: 15 * time.Minute}}
		}),
		Source:   silentSource{},
		Logger:   slog.New(slog.DiscardHandler),
		Location: tokyo,
	}
	done := make(chan error)
	go func() { done <- p.Run(ctx) }()

	clock := &fakeClock{now: at(14, 6, 0, 0), end: at(14, 7, 0, 0), cancel: cancel}
	player := &fakePlayer{clock: clock}
	s := &Scheduler{
		Clock: clock,
		Settings: sourceFunc(func() settings.Settings {
			return settings.Settings{Alarm: alarmAt(6, 30), Calendar: settings.Calendar{LeadTime: 10 * time.Minute}}
		}),
		Events:   p,
		Player:   player,
		Logger:   slog.New(slog.DiscardHandler),
		Location: tokyo,
	}
	if err := s.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Scheduler.Run = %v, want %v", err, context.Canceled)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Poller.Run = %v, want %v", err, context.Canceled)
	}
	plays := player.plays()
	if len(plays) != 1 {
		t.Fatalf("Play calls = %d, want 1: %+v", len(plays), plays)
	}
	checkStart(t, plays[0].at, at(14, 6, 30, 0))
}

func TestPollerPassesCalendarsAndDays(t *testing.T) {
	tests := []struct {
		name      string
		calendars []string
	}{
		{"listed calendars", []string{"primary", "team@example.com"}},
		{"no list", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeSource{results: []sourceResult{{}}}
			runPoller(t, at(14, 23, 50, 0), at(15, 0, 0, 0), func(time.Time) settings.Calendar {
				return settings.Calendar{ReadInterval: 15 * time.Minute, Calendars: tt.calendars}
			}, src)
			if len(src.calls) != 1 {
				t.Fatalf("reads = %+v, want 1", src.calls)
			}
			c := src.calls[0]
			if !slices.Equal(c.calendars, tt.calendars) {
				t.Errorf("calendars = %q, want %q", c.calendars, tt.calendars)
			}
			if !c.from.Equal(at(14, 0, 0, 0)) || !c.to.Equal(at(16, 0, 0, 0)) {
				t.Errorf("read from %v to %v, want from %v to %v", c.from, c.to, at(14, 0, 0, 0), at(16, 0, 0, 0))
			}
		})
	}
}

func TestPollerUsesChangedReadInterval(t *testing.T) {
	src := &fakeSource{results: []sourceResult{{}}}
	runPoller(t, at(14, 5, 0, 0), at(14, 6, 12, 0), func(now time.Time) settings.Calendar {
		if now.Before(at(14, 5, 30, 0)) {
			return settings.Calendar{ReadInterval: 15 * time.Minute}
		}
		return settings.Calendar{ReadInterval: 20 * time.Minute}
	}, src)
	var got []time.Time
	for _, c := range src.calls {
		got = append(got, c.at)
	}
	want := []time.Time{at(14, 5, 0, 0), at(14, 5, 15, 0), at(14, 5, 30, 0), at(14, 5, 50, 0), at(14, 6, 10, 0)}
	if !slices.EqualFunc(got, want, time.Time.Equal) {
		t.Errorf("reads at %v, want %v", got, want)
	}
}
