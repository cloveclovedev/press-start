package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validFile = `
[alarm]
sound_file = "/sounds/alarm.mp3"
length = "90s"
volume = 70

[alarm.weekdays]
monday = "06:30"
tuesday = "none"
saturday = "08:05"

[alarm.dates]
"2026-12-24" = "05:45"
2026-12-25 = "none"

[calendar]
lead_time = "25m"
`

func TestParseValid(t *testing.T) {
	s, err := Parse([]byte(validFile))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a := s.Alarm
	if a.SoundFile != "/sounds/alarm.mp3" {
		t.Errorf("SoundFile = %q, want %q", a.SoundFile, "/sounds/alarm.mp3")
	}
	if a.Length != 90*time.Second {
		t.Errorf("Length = %v, want 1m30s", a.Length)
	}
	if a.Volume != 70 {
		t.Errorf("Volume = %d, want 70", a.Volume)
	}
	if s.Calendar.LeadTime != 25*time.Minute {
		t.Errorf("LeadTime = %v, want 25m0s", s.Calendar.LeadTime)
	}

	wantWeekdays := [7]AlarmSetting{
		time.Monday:   {On: true, Hour: 6, Minute: 30},
		time.Saturday: {On: true, Hour: 8, Minute: 5},
	}
	if a.Weekdays != wantWeekdays {
		t.Errorf("Weekdays = %+v, want %+v", a.Weekdays, wantWeekdays)
	}

	wantDates := map[Date]AlarmSetting{
		{2026, time.December, 24}: {On: true, Hour: 5, Minute: 45},
		{2026, time.December, 25}: {},
	}
	if len(a.Dates) != len(wantDates) {
		t.Fatalf("Dates = %+v, want %+v", a.Dates, wantDates)
	}
	for d, want := range wantDates {
		got, ok := a.Dates[d]
		if !ok || got != want {
			t.Errorf("Dates[%s] = %+v (present %v), want %+v", d, got, ok, want)
		}
	}
}

func TestParseDefaultLength(t *testing.T) {
	s, err := Parse([]byte("[alarm]\nsound_file = \"a.wav\"\nvolume = 0\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Alarm.Length != 3*time.Minute {
		t.Errorf("Length = %v, want 3m0s", s.Alarm.Length)
	}
	if s.Calendar.LeadTime != 10*time.Minute {
		t.Errorf("LeadTime = %v, want 10m0s", s.Calendar.LeadTime)
	}
	if s.Alarm.Weekdays != [7]AlarmSetting{} {
		t.Errorf("Weekdays = %+v, want all off", s.Alarm.Weekdays)
	}
}

func TestParseSoundFileExtension(t *testing.T) {
	for _, name := range []string{"a.mp3", "a.MP3", "a.wav", "a.Wav"} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte("[alarm]\nsound_file = \"" + name + "\"\nvolume = 50\n"))
			if err != nil {
				t.Errorf("Parse: %v", err)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	const base = "[alarm]\nsound_file = \"a.mp3\"\nvolume = 50\n"
	tests := []struct {
		name    string
		file    string
		wantKey string
	}{
		{"unknown top key", base + "[other]\nx = 1\n", "other"},
		{"unknown alarm key", "[alarm]\nsound_file = \"a.mp3\"\nvolume = 50\nloud = true\n", "alarm.loud"},
		{"bad time hour", base + "[alarm.weekdays]\nmonday = \"24:00\"\n", "alarm.weekdays.monday"},
		{"bad time minute", base + "[alarm.weekdays]\nfriday = \"07:60\"\n", "alarm.weekdays.friday"},
		{"bad time form", base + "[alarm.weekdays]\nmonday = \"7:00\"\n", "alarm.weekdays.monday"},
		{"bad time word", base + "[alarm.weekdays]\nmonday = \"off\"\n", "alarm.weekdays.monday"},
		{"bad date time", base + "[alarm.dates]\n\"2026-12-24\" = \"25:00\"\n", "alarm.dates.\"2026-12-24\""},
		{"bad date", base + "[alarm.dates]\n\"2026-02-30\" = \"07:00\"\n", "alarm.dates.\"2026-02-30\""},
		{"bad date form", base + "[alarm.dates]\n\"12/24\" = \"07:00\"\n", "alarm.dates.\"12/24\""},
		{"bad weekday", base + "[alarm.weekdays]\nmon = \"07:00\"\n", "alarm.weekdays.mon"},
		{"zero length", base + "length = \"0s\"\n", "alarm.length"},
		{"negative length", base + "length = \"-1m\"\n", "alarm.length"},
		{"bad length", base + "length = \"three\"\n", "alarm.length"},
		{"zero lead time", base + "[calendar]\nlead_time = \"0s\"\n", "calendar.lead_time"},
		{"negative lead time", base + "[calendar]\nlead_time = \"-5m\"\n", "calendar.lead_time"},
		{"bad lead time", base + "[calendar]\nlead_time = \"ten\"\n", "calendar.lead_time"},
		{"unknown calendar key", base + "[calendar]\ncalendars = []\n", "calendar.calendars"},
		{"missing volume", "[alarm]\nsound_file = \"a.mp3\"\n", "alarm.volume"},
		{"volume too high", "[alarm]\nsound_file = \"a.mp3\"\nvolume = 101\n", "alarm.volume"},
		{"volume too low", "[alarm]\nsound_file = \"a.mp3\"\nvolume = -1\n", "alarm.volume"},
		{"bad sound file", "[alarm]\nsound_file = \"a.ogg\"\nvolume = 50\n", "alarm.sound_file"},
		{"missing sound file", "[alarm]\nvolume = 50\n", "alarm.sound_file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.file))
			if err == nil {
				t.Fatal("Parse: got no error")
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Errorf("error %q does not name %s", err, tt.wantKey)
			}
		})
	}
}

func TestLoadRelativeSoundFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	want := filepath.Join(dir, "sounds", "a.mp3")
	if err := os.Mkdir(filepath.Join(dir, "sounds"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, want, "")
	writeFile(t, path, "[alarm]\nsound_file = \"sounds/a.mp3\"\nvolume = 50\n")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Alarm.SoundFile != want {
		t.Errorf("SoundFile = %q, want %q", s.Alarm.SoundFile, want)
	}
}

func TestLoadAbsoluteSoundFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	abs := filepath.Join(t.TempDir(), "a.wav")
	writeFile(t, abs, "")
	writeFile(t, path, "[alarm]\nsound_file = '"+abs+"'\nvolume = 50\n")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Alarm.SoundFile != abs {
		t.Errorf("SoundFile = %q, want %q", s.Alarm.SoundFile, abs)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Fatal("Load: got no error")
	}
}

func TestLoadSoundFileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	writeFile(t, path, "[alarm]\nsound_file = \"missing.mp3\"\nvolume = 50\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: got no error")
	}
	if !strings.Contains(err.Error(), "alarm.sound_file") {
		t.Errorf("error %q does not name alarm.sound_file", err)
	}
}

func TestLoadSoundFileNotRegular(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	if err := os.Mkdir(filepath.Join(dir, "dir.mp3"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "[alarm]\nsound_file = \"dir.mp3\"\nvolume = 50\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: got no error")
	}
	if !strings.Contains(err.Error(), "alarm.sound_file") {
		t.Errorf("error %q does not name alarm.sound_file", err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
