package alarm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/mkuri/press-start/internal/core/settings"
	"github.com/mkuri/press-start/internal/platform/alsa"
)

var (
	_ SettingsSource = (*settings.Store)(nil)
	_ Player         = (*alsa.Player)(nil)
)

var tokyo = time.FixedZone("JST", 9*60*60)

// fakeClock moves its time forward only when the scheduler waits or plays.
// After it passes end, it cancels the run and never fires again.
type fakeClock struct {
	now    time.Time
	end    time.Time
	cancel context.CancelFunc
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.now = c.now.Add(d)
	if !c.now.Before(c.end) {
		c.cancel()
		return nil
	}
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

type playerCall struct {
	name   string
	at     time.Time
	volume int
	file   string
	length time.Duration
}

// fakePlayer records its calls. Play takes the length on the fake clock.
type fakePlayer struct {
	clock     *fakeClock
	calls     []playerCall
	volumeErr error
}

func (p *fakePlayer) SetVolume(_ context.Context, percent int) error {
	p.calls = append(p.calls, playerCall{name: "SetVolume", at: p.clock.now, volume: percent})
	return p.volumeErr
}

func (p *fakePlayer) Play(_ context.Context, file string, length time.Duration) error {
	p.calls = append(p.calls, playerCall{name: "Play", at: p.clock.now, file: file, length: length})
	p.clock.now = p.clock.now.Add(length)
	return nil
}

func (p *fakePlayer) plays() []playerCall {
	var plays []playerCall
	for _, c := range p.calls {
		if c.name == "Play" {
			plays = append(plays, c)
		}
	}
	return plays
}

// sourceFunc gives the settings for the time of the fake clock.
type sourceFunc func() settings.Settings

func (f sourceFunc) Current() settings.Settings { return f() }

type schedulerFixture struct {
	clock  *fakeClock
	player *fakePlayer
	log    bytes.Buffer
}

// runScheduler runs a Scheduler on a fake clock from start to end.
func runScheduler(t *testing.T, start, end time.Time, current func(now time.Time) settings.Alarm, volumeErr error) *schedulerFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &schedulerFixture{clock: &fakeClock{now: start, end: end, cancel: cancel}}
	f.player = &fakePlayer{clock: f.clock, volumeErr: volumeErr}
	s := &Scheduler{
		Clock: f.clock,
		Settings: sourceFunc(func() settings.Settings {
			return settings.Settings{Alarm: current(f.clock.now)}
		}),
		Player:   f.player,
		Logger:   slog.New(slog.NewJSONHandler(&f.log, nil)),
		Location: tokyo,
	}
	if err := s.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want %v", err, context.Canceled)
	}
	return f
}

type logLine struct {
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	Msg      string    `json:"msg"`
	Date     string    `json:"date"`
	WakeTime string    `json:"wake_time"`
}

func (f *schedulerFixture) lines(t *testing.T) []logLine {
	t.Helper()
	var lines []logLine
	sc := bufio.NewScanner(&f.log)
	for sc.Scan() {
		var l logLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}
		lines = append(lines, l)
	}
	return lines
}

func at(day, hour, minute, second int) time.Time {
	return time.Date(2026, time.December, day, hour, minute, second, 0, tokyo)
}

func alarmAt(hour, minute int) settings.Alarm {
	a := settings.Alarm{SoundFile: "alarm.mp3", Length: 3 * time.Minute, Volume: 60}
	for d := range a.Weekdays {
		a.Weekdays[d] = settings.AlarmSetting{On: true, Hour: hour, Minute: minute}
	}
	return a
}

func checkStart(t *testing.T, got, wake time.Time) {
	t.Helper()
	if got.Before(wake) || got.Sub(wake) >= time.Minute {
		t.Errorf("Play at %v, want within 1 minute of %v", got, wake)
	}
}

func TestSchedulerStartsOncePerDate(t *testing.T) {
	// 2026-12-14 is a Monday, and 2026-12-16 a Wednesday without a setting.
	f := runScheduler(t, at(14, 5, 59, 53), at(16, 12, 0, 0), func(time.Time) settings.Alarm {
		return testAlarm()
	}, nil)
	plays := f.player.plays()
	if len(plays) != 2 {
		t.Fatalf("Play calls = %d, want 2: %+v", len(plays), plays)
	}
	checkStart(t, plays[0].at, at(14, 6, 30, 0))
	checkStart(t, plays[1].at, at(15, 7, 0, 0))
}

func TestSchedulerShortSoundStartsOnce(t *testing.T) {
	// The sound ends inside the start window, and must not start again.
	f := runScheduler(t, at(14, 6, 0, 0), at(14, 8, 0, 0), func(time.Time) settings.Alarm {
		a := alarmAt(6, 30)
		a.Length = 10 * time.Second
		return a
	}, nil)
	if plays := f.player.plays(); len(plays) != 1 {
		t.Fatalf("Play calls = %d, want 1: %+v", len(plays), plays)
	}
}

func TestSchedulerNoAlarmNeverPlays(t *testing.T) {
	f := runScheduler(t, at(14, 0, 0, 0), at(16, 0, 0, 0), func(time.Time) settings.Alarm {
		return settings.Alarm{SoundFile: "alarm.mp3", Length: 3 * time.Minute, Volume: 60}
	}, nil)
	if len(f.player.calls) != 0 {
		t.Fatalf("player calls = %+v, want none", f.player.calls)
	}
	var decided []logLine
	for _, l := range f.lines(t) {
		if l.Msg == "wake time decided" {
			decided = append(decided, l)
		}
	}
	if len(decided) != 2 || decided[0].Date != "2026-12-14" || decided[1].Date != "2026-12-15" {
		t.Fatalf("wake time decided = %+v, want 2026-12-14 and 2026-12-15", decided)
	}
	for _, l := range decided {
		if l.WakeTime != "none" {
			t.Errorf("%s: wake_time = %q, want none", l.Date, l.WakeTime)
		}
	}
}

func TestSchedulerUsesChangedSettings(t *testing.T) {
	tests := []struct {
		name     string
		changeAt time.Time
		before   settings.Alarm
		after    settings.Alarm
		wake     time.Time
	}{
		{"earlier", at(14, 6, 0, 0), alarmAt(7, 0), alarmAt(6, 30), at(14, 6, 30, 0)},
		{"later", at(14, 6, 20, 0), alarmAt(6, 30), alarmAt(6, 45), at(14, 6, 45, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := runScheduler(t, at(14, 5, 0, 0), at(14, 9, 0, 0), func(now time.Time) settings.Alarm {
				if now.Before(tt.changeAt) {
					return tt.before
				}
				return tt.after
			}, nil)
			plays := f.player.plays()
			if len(plays) != 1 {
				t.Fatalf("Play calls = %d, want 1: %+v", len(plays), plays)
			}
			checkStart(t, plays[0].at, tt.wake)

			var wakeTimes []string
			for _, l := range f.lines(t) {
				if l.Msg == "wake time decided" {
					wakeTimes = append(wakeTimes, l.WakeTime)
				}
			}
			want := []string{
				time.Date(2026, time.December, 14, tt.before.Weekdays[time.Monday].Hour, tt.before.Weekdays[time.Monday].Minute, 0, 0, tokyo).Format(time.RFC3339),
				tt.wake.Format(time.RFC3339),
			}
			if len(wakeTimes) != 2 || wakeTimes[0] != want[0] || wakeTimes[1] != want[1] {
				t.Errorf("decided wake times = %v, want %v", wakeTimes, want)
			}
		})
	}
}

func TestSchedulerSetsVolumeBeforePlay(t *testing.T) {
	tests := []struct {
		name      string
		volumeErr error
	}{
		{"volume set", nil},
		{"volume fails", errors.New("amixer failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := runScheduler(t, at(14, 6, 0, 0), at(14, 8, 0, 0), func(time.Time) settings.Alarm {
				a := alarmAt(6, 30)
				a.Volume = 70
				a.Length = 2 * time.Minute
				return a
			}, tt.volumeErr)
			calls := f.player.calls
			if len(calls) != 2 || calls[0].name != "SetVolume" || calls[1].name != "Play" {
				t.Fatalf("player calls = %+v, want SetVolume then Play", calls)
			}
			if calls[0].volume != 70 {
				t.Errorf("SetVolume(%d), want 70", calls[0].volume)
			}
			if calls[1].file != "alarm.mp3" || calls[1].length != 2*time.Minute {
				t.Errorf("Play(%q, %v), want (alarm.mp3, 2m0s)", calls[1].file, calls[1].length)
			}
			failed := 0
			for _, l := range f.lines(t) {
				if l.Msg == "set volume failed" && l.Level == "ERROR" {
					failed++
				}
			}
			want := 0
			if tt.volumeErr != nil {
				want = 1
			}
			if failed != want {
				t.Errorf("set volume failed lines = %d, want %d", failed, want)
			}
		})
	}
}

func TestSchedulerRecordsEventsInOrder(t *testing.T) {
	start := at(14, 6, 29, 50)
	f := runScheduler(t, start, at(14, 7, 0, 0), func(time.Time) settings.Alarm {
		return alarmAt(6, 30)
	}, nil)
	playAt := f.player.plays()[0].at
	want := []logLine{
		{Time: start, Msg: "wake time decided", Date: "2026-12-14", WakeTime: at(14, 6, 30, 0).Format(time.RFC3339)},
		{Time: playAt, Msg: "alarm sound started", Date: "2026-12-14"},
		{Time: playAt.Add(3 * time.Minute), Msg: "alarm sound ended", Date: "2026-12-14"},
	}
	got := f.lines(t)
	if len(got) != len(want) {
		t.Fatalf("log = %+v, want %d lines", got, len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Msg != w.Msg || g.Date != w.Date || g.WakeTime != w.WakeTime || !g.Time.Equal(w.Time) {
			t.Errorf("log line %d = %+v, want %+v", i, g, w)
		}
	}
}
