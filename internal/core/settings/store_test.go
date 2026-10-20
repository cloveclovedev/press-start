package settings

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeFixture holds a settings file next to a sound file, and a logger that
// writes to a buffer.
type storeFixture struct {
	path string
	log  bytes.Buffer
}

func newStoreFixture(t *testing.T, contents string) *storeFixture {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.mp3"), "")
	f := &storeFixture{path: filepath.Join(dir, "settings.toml")}
	writeFile(t, f.path, contents)
	return f
}

func (f *storeFixture) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&f.log, nil))
}

func (f *storeFixture) unreadableLines() int {
	return strings.Count(f.log.String(), `"msg":"settings unreadable"`)
}

func volumeFile(volume string) string {
	return "[alarm]\nsound_file = \"a.mp3\"\nvolume = " + volume + "\n"
}

func newTestStore(t *testing.T, f *storeFixture) *Store {
	t.Helper()
	st, err := NewStore(f.path, f.logger())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st
}

func TestStoreReadsChangedFile(t *testing.T) {
	f := newStoreFixture(t, volumeFile("10"))
	st := newTestStore(t, f)
	if got := st.Current().Alarm.Volume; got != 10 {
		t.Fatalf("Volume = %d, want 10", got)
	}
	writeFile(t, f.path, volumeFile("20"))
	if got := st.Current().Alarm.Volume; got != 20 {
		t.Errorf("Volume after change = %d, want 20", got)
	}
}

func TestStoreKeepsLastGoodSettings(t *testing.T) {
	tests := []struct {
		name      string
		breakFile func(t *testing.T, f *storeFixture)
	}{
		{"invalid TOML", func(t *testing.T, f *storeFixture) {
			writeFile(t, f.path, "[alarm\nvolume = ")
		}},
		{"removed file", func(t *testing.T, f *storeFixture) {
			if err := os.Remove(f.path); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing sound file", func(t *testing.T, f *storeFixture) {
			writeFile(t, f.path, "[alarm]\nsound_file = \"missing.mp3\"\nvolume = 30\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStoreFixture(t, volumeFile("10"))
			st := newTestStore(t, f)
			writeFile(t, f.path, volumeFile("20"))
			st.Current()

			tt.breakFile(t, f)
			if got := st.Current().Alarm.Volume; got != 20 {
				t.Errorf("Volume = %d, want the last good 20", got)
			}
			if f.unreadableLines() != 1 {
				t.Errorf("log has %d settings unreadable lines, want 1:\n%s", f.unreadableLines(), f.log.String())
			}
			if !strings.Contains(f.log.String(), `"path":`) || !strings.Contains(f.log.String(), `"error":`) {
				t.Errorf("log does not have the path and the error:\n%s", f.log.String())
			}
		})
	}
}

func TestStoreLogsOncePerUnreadablePeriod(t *testing.T) {
	f := newStoreFixture(t, volumeFile("10"))
	st := newTestStore(t, f)

	writeFile(t, f.path, "not toml [")
	for range 3 {
		st.Current()
	}
	if got := f.unreadableLines(); got != 1 {
		t.Fatalf("log has %d settings unreadable lines while the file stays invalid, want 1", got)
	}

	writeFile(t, f.path, volumeFile("40"))
	if got := st.Current().Alarm.Volume; got != 40 {
		t.Fatalf("Volume after fix = %d, want 40", got)
	}
	writeFile(t, f.path, "not toml [")
	st.Current()
	st.Current()
	if got := f.unreadableLines(); got != 2 {
		t.Errorf("log has %d settings unreadable lines after the file breaks again, want 2", got)
	}
}

func TestNewStoreFails(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		var log bytes.Buffer
		if _, err := NewStore(filepath.Join(t.TempDir(), "missing.toml"), slog.New(slog.NewJSONHandler(&log, nil))); err == nil {
			t.Error("NewStore: got no error")
		}
	})
	t.Run("invalid file", func(t *testing.T) {
		f := newStoreFixture(t, "not toml [")
		if _, err := NewStore(f.path, f.logger()); err == nil {
			t.Error("NewStore: got no error")
		}
	})
}
