package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkuri/press-start/internal/alarm"
)

// syncBuffer is a bytes.Buffer that the test reads while run writes.
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

// fakePlayer plays nothing.
type fakePlayer struct{}

func (fakePlayer) SetVolume(context.Context, int) error              { return nil }
func (fakePlayer) Play(context.Context, string, time.Duration) error { return nil }

func newFakePlayer(config) alarm.Player { return fakePlayer{} }

// writeSettings writes a valid settings file and its sound file, and returns
// the path of the settings file.
func writeSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alarm.mp3"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.toml")
	data := "[alarm]\nsound_file = \"alarm.mp3\"\nvolume = 50\n\n[alarm.weekdays]\nmonday = \"07:00\"\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func validEnv(settingsPath string) map[string]string {
	return map[string]string{
		envSettings:     settingsPath,
		envALSADevice:   "default",
		envMixerCard:    "0",
		envMixerControl: "Master",
	}
}

func TestRunNamesEachMissingVariable(t *testing.T) {
	names := []string{envSettings, envALSADevice, envMixerCard, envMixerControl}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			vars := validEnv(writeSettings(t))
			delete(vars, name)
			var out syncBuffer
			err := runWith(context.Background(), env(vars), &out, newFakePlayer)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("run = %v, want an error that names %s", err, name)
			}
		})
	}
	t.Run("all", func(t *testing.T) {
		err := run(context.Background(), env(nil), &syncBuffer{})
		if err == nil {
			t.Fatal("run = nil, want an error")
		}
		for _, name := range names {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("run = %v, want an error that names %s", err, name)
			}
		}
	})
}

func TestRunFailsWhenSettingsDoNotLoad(t *testing.T) {
	tests := map[string]string{
		"missing file": filepath.Join(t.TempDir(), "missing.toml"),
		"bad file":     filepath.Join(t.TempDir(), "bad.toml"),
	}
	if err := os.WriteFile(tests["bad file"], []byte("not toml ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			err := runWith(context.Background(), env(validEnv(path)), &syncBuffer{}, newFakePlayer)
			if err == nil || !strings.Contains(err.Error(), "load settings") {
				t.Fatalf("run = %v, want a load settings error", err)
			}
		})
	}
}

func TestRunRecordsWakeTimeAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- runWith(ctx, env(validEnv(writeSettings(t))), &out, newFakePlayer)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "wake time decided") {
		if time.Now().After(deadline) {
			t.Fatalf("no wake time decided line in %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after the context ended")
	}

	sc := bufio.NewScanner(strings.NewReader(out.String()))
	sc.Scan()
	var line struct {
		Msg      string `json:"msg"`
		Date     string `json:"date"`
		WakeTime string `json:"wake_time"`
	}
	if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
		t.Fatalf("first line %q is not JSON: %v", sc.Text(), err)
	}
	if line.Msg != "wake time decided" || line.Date == "" || line.WakeTime == "" {
		t.Fatalf("first line = %+v, want a wake time decided line with date and wake_time", line)
	}
}
