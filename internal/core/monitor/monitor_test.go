package monitor

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkuri/press-start/internal/core/settings"
)

var _ SettingsSource = (*settings.Store)(nil)

var start = time.Date(2026, time.October, 20, 6, 0, 0, 0, time.UTC)

// wait is one call to After that Run waits on.
type wait struct {
	d  time.Duration
	ch chan time.Time
}

// fakeClock hands each call to After to the test, so that the test decides
// when the wait ends and what time it is then.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits chan wait
	stop  chan struct{}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	select {
	case c.waits <- wait{d: d, ch: ch}:
	case <-c.stop:
	}
	return ch
}

func (c *fakeClock) advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

type notification struct {
	at   time.Time
	fail bool
	body string
}

// fakeNotifier records each notification at the time of the clock. With
// block set, it waits until the test ends.
type fakeNotifier struct {
	clock   *fakeClock
	err     error
	block   bool
	entered chan struct{}
	release chan struct{}

	mu   sync.Mutex
	sent []notification
}

func (n *fakeNotifier) Success(context.Context) error {
	return n.notify(notification{at: n.clock.Now()})
}

func (n *fakeNotifier) Fail(_ context.Context, body string) error {
	return n.notify(notification{at: n.clock.Now(), fail: true, body: body})
}

func (n *fakeNotifier) notify(x notification) error {
	n.mu.Lock()
	n.sent = append(n.sent, x)
	n.mu.Unlock()
	if n.block {
		n.entered <- struct{}{}
		<-n.release
	}
	return n.err
}

func (n *fakeNotifier) notifications() []notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]notification(nil), n.sent...)
}

// intervalSource gives settings with an interval that the test can change.
type intervalSource struct {
	mu       sync.Mutex
	interval time.Duration
}

func (s *intervalSource) Current() settings.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return settings.Settings{Monitor: settings.Monitor{Interval: s.interval}}
}

func (s *intervalSource) set(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interval = d
}

type fixture struct {
	t        *testing.T
	clock    *fakeClock
	notifier *fakeNotifier
	source   *intervalSource
	monitor  *Monitor
	log      *syncBuffer
}

// syncBuffer is a bytes.Buffer that Run and the test can use at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newFixture returns a Monitor on a fake clock with an interval of 5m. The
// test calls run to start Run.
func newFixture(t *testing.T, notifier *fakeNotifier) *fixture {
	t.Helper()
	f := &fixture{
		t:        t,
		clock:    &fakeClock{now: start, waits: make(chan wait), stop: make(chan struct{})},
		notifier: notifier,
		source:   &intervalSource{interval: 5 * time.Minute},
		log:      &syncBuffer{},
	}
	notifier.clock = f.clock
	f.monitor = New(f.clock, f.source, notifier, slog.New(slog.NewJSONHandler(f.log, nil)))
	return f
}

// run starts Run and stops it when the test ends.
func (f *fixture) run() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.monitor.Run(ctx) }()
	f.t.Cleanup(func() {
		cancel()
		close(f.clock.stop)
		if f.notifier.release != nil {
			close(f.notifier.release)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			f.t.Errorf("Run = %v, want context.Canceled", err)
		}
	})
}

// nextWait returns the next wait of Run.
func (f *fixture) nextWait() wait {
	f.t.Helper()
	select {
	case w := <-f.clock.waits:
		return w
	case <-time.After(5 * time.Second):
		f.t.Fatal("Run does not wait")
		return wait{}
	}
}

// wantWait reads the next wait of Run and checks its length.
func (f *fixture) wantWait(d time.Duration) wait {
	f.t.Helper()
	w := f.nextWait()
	if w.d != d {
		f.t.Fatalf("wait = %v, want %v", w.d, d)
	}
	return w
}

// fire ends the wait w at its end time.
func (f *fixture) fire(w wait) {
	w.ch <- f.clock.advance(w.d)
}

func (f *fixture) wantSent(want ...notification) {
	f.t.Helper()
	got := f.notifier.notifications()
	if len(got) != len(want) {
		f.t.Fatalf("notifications = %+v, want %+v", got, want)
	}
	for i := range want {
		if !got[i].at.Equal(want[i].at) || got[i].fail != want[i].fail || got[i].body != want[i].body {
			f.t.Errorf("notification %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func success(after time.Duration) notification {
	return notification{at: start.Add(after)}
}

func failure(after time.Duration, body string) notification {
	return notification{at: start.Add(after), fail: true, body: body}
}

func TestFailAndLastClearEachSendOnce(t *testing.T) {
	f := newFixture(t, &fakeNotifier{})
	f.run()
	f.wantWait(5 * time.Minute)

	f.clock.advance(time.Minute)
	f.monitor.Fail("calendar read failed")
	f.wantWait(5 * time.Minute)
	f.monitor.Fail("calendar read failed")

	f.clock.advance(time.Minute)
	f.monitor.Clear("calendar read failed")
	f.wantWait(5 * time.Minute)

	f.wantSent(
		success(0),
		failure(time.Minute, "calendar read failed"),
		success(2*time.Minute),
	)
}

func TestRegularNotificationSendsTheCurrentState(t *testing.T) {
	f := newFixture(t, &fakeNotifier{})
	f.monitor.Fail("settings file unreadable")
	f.monitor.Fail("alarm sound failed")
	f.run()

	w := f.wantWait(5 * time.Minute)
	f.fire(w)
	w = f.wantWait(5 * time.Minute)

	// The list shrinks, but a failure still lasts, so nothing is sent now.
	f.monitor.Clear("alarm sound failed")
	f.fire(w)
	f.wantWait(5 * time.Minute)

	f.clock.advance(time.Minute)
	f.monitor.Clear("settings file unreadable")
	w = f.wantWait(5 * time.Minute)
	f.fire(w)
	f.wantWait(5 * time.Minute)

	f.wantSent(
		failure(0, "alarm sound failed\nsettings file unreadable"),
		failure(5*time.Minute, "alarm sound failed\nsettings file unreadable"),
		failure(10*time.Minute, "settings file unreadable"),
		success(11*time.Minute),
		success(16*time.Minute),
	)
}

func TestChangesInsideTheGapSendOnceAtItsEnd(t *testing.T) {
	f := newFixture(t, &fakeNotifier{})
	f.run()
	f.wantWait(5 * time.Minute)

	f.clock.advance(3 * time.Second)
	f.monitor.Fail("calendar read failed")
	f.wantWait(9 * time.Second)

	f.clock.advance(4 * time.Second)
	f.monitor.Fail("alarm sound failed")
	w := f.wantWait(5 * time.Second)
	f.fire(w)
	f.wantWait(5 * time.Minute)

	f.wantSent(
		success(0),
		failure(12*time.Second, "alarm sound failed\ncalendar read failed"),
	)
}

func TestReportDoesNotWaitForTheNotifier(t *testing.T) {
	n := &fakeNotifier{block: true, entered: make(chan struct{}), release: make(chan struct{})}
	f := newFixture(t, n)
	f.run()
	select {
	case <-n.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Run does not notify")
	}

	done := make(chan struct{})
	go func() {
		for range 100 {
			f.monitor.Fail("calendar read failed")
			f.monitor.Fail("alarm sound failed")
			f.monitor.Clear("calendar read failed")
			f.monitor.Clear("alarm sound failed")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Fail and Clear wait for the notifier")
	}
}

func TestNotifierErrorIsRecorded(t *testing.T) {
	f := newFixture(t, &fakeNotifier{err: errors.New("monitor does not answer")})
	f.run()
	w := f.wantWait(5 * time.Minute)

	log := f.log.String()
	if !strings.Contains(log, `"msg":"monitor notify failed"`) || !strings.Contains(log, `"error":"monitor does not answer"`) {
		t.Errorf("log = %s, want monitor notify failed with the error", log)
	}

	f.fire(w)
	f.wantWait(5 * time.Minute)
	f.wantSent(success(0), success(5*time.Minute))
}

func TestChangedIntervalChangesTheWait(t *testing.T) {
	f := newFixture(t, &fakeNotifier{})
	f.run()
	w := f.wantWait(5 * time.Minute)

	f.source.set(time.Minute)
	f.fire(w)
	w = f.wantWait(time.Minute)
	f.fire(w)
	f.wantWait(time.Minute)

	f.wantSent(success(0), success(5*time.Minute), success(6*time.Minute))
}
