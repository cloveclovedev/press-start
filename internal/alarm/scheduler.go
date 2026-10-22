package alarm

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mkuri/press-start/internal/core/settings"
)

// CheckInterval is how often the scheduler decides the wake time.
const CheckInterval = 15 * time.Second

// Clock tells the time and waits. Tests replace it so that they take no real
// time.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the Clock of the time package.
type SystemClock struct{}

// Now returns time.Now().
func (SystemClock) Now() time.Time { return time.Now() }

// After returns time.After(d).
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// SettingsSource gives the current settings. *settings.Store is one.
type SettingsSource interface {
	Current() settings.Settings
}

// Player sets the speaker volume and plays the alarm sound.
type Player interface {
	SetVolume(ctx context.Context, percent int) error
	Play(ctx context.Context, file string, length time.Duration) error
}

// Scheduler starts the wake-up run at the wake time of each day.
type Scheduler struct {
	Clock    Clock
	Settings SettingsSource
	Player   Player
	Logger   *slog.Logger
	// Location is the time zone of the wake time.
	Location *time.Location

	// started is the time when Run started. A wake time before it stays
	// silent, because after a restart nothing tells whether the run of that
	// date already started.
	started    time.Time
	decided    bool
	lastDate   settings.Date
	lastWake   time.Time
	lastWakeOK bool
	ranDate    settings.Date
	ran        bool
}

// Run decides the wake time every CheckInterval until ctx ends. It reads the
// settings at each decision, so a changed file is used without a restart. It
// starts the wake-up run once for each date, at the first decision on or
// after the wake time. A wake time that moves earlier into the past still
// starts the run, so that a late change still wakes the user. A wake time
// before the start of Run never starts the run. A day without a wake time has
// no wake-up run. Run returns ctx.Err().
func (s *Scheduler) Run(ctx context.Context) error {
	s.started = s.Clock.Now()
	for {
		s.check(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.Clock.After(CheckInterval):
		}
	}
}

func (s *Scheduler) check(ctx context.Context) {
	now := s.Clock.Now().In(s.Location)
	y, m, d := now.Date()
	date := settings.Date{Year: y, Month: m, Day: d}
	a := s.Settings.Current().Alarm
	wake, ok := WakeTime(date, s.Location, a)

	if !s.decided || date != s.lastDate || ok != s.lastWakeOK || !wake.Equal(s.lastWake) {
		s.decided, s.lastDate, s.lastWake, s.lastWakeOK = true, date, wake, ok
		wakeAttr := "none"
		if ok {
			wakeAttr = wake.Format(time.RFC3339)
		}
		s.record(ctx, slog.LevelInfo, "wake time decided", "date", date.String(), "wake_time", wakeAttr)
	}

	if !ok || now.Before(wake) || wake.Before(s.started) {
		return
	}
	if s.ran && s.ranDate == date {
		return
	}
	s.ran, s.ranDate = true, date
	s.wakeUp(ctx, date, a)
}

// wakeUp is the wake-up run: it sets the volume, then plays the alarm sound
// for its length. The sound plays also when the volume cannot be set. When
// the run ends because the service stops, the sound stopped as expected, so
// the record is a stop and not a failure.
func (s *Scheduler) wakeUp(ctx context.Context, date settings.Date, a settings.Alarm) {
	s.record(ctx, slog.LevelInfo, "alarm sound started", "date", date.String())
	if err := s.Player.SetVolume(ctx, a.Volume); err != nil {
		s.record(ctx, slog.LevelError, "set volume failed", "volume", a.Volume, "error", err)
	}
	if err := s.Player.Play(ctx, a.SoundFile, a.Length); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			s.record(ctx, slog.LevelInfo, "alarm sound stopped", "reason", "service stopping")
		} else {
			s.record(ctx, slog.LevelError, "play failed", "error", err)
		}
	}
	s.record(ctx, slog.LevelInfo, "alarm sound ended", "date", date.String())
}

// record logs msg at the time of the Clock.
func (s *Scheduler) record(ctx context.Context, level slog.Level, msg string, args ...any) {
	h := s.Logger.Handler()
	if !h.Enabled(ctx, level) {
		return
	}
	r := slog.NewRecord(s.Clock.Now(), level, msg, 0)
	r.Add(args...)
	_ = h.Handle(ctx, r)
}
