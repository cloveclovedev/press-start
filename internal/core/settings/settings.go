// Package settings reads the settings file of press-start into checked values.
//
// The settings file is one TOML file. It holds an alarm setting for each
// weekday and for single dates, and how the calendar events are used.
package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultLength is the length of the alarm sound when the file sets none.
const DefaultLength = 3 * time.Minute

// DefaultLeadTime is the lead time when the file sets none.
const DefaultLeadTime = 10 * time.Minute

// DefaultReadInterval is how often the calendar is read when the file sets
// none.
const DefaultReadInterval = 15 * time.Minute

// Settings holds the checked contents of the settings file.
type Settings struct {
	Alarm    Alarm
	Calendar Calendar
}

// Calendar holds how the calendar events decide the wake time.
type Calendar struct {
	// LeadTime is how long before the start of the first event the user
	// wakes.
	LeadTime time.Duration
	// ReadInterval is how long the poller waits between two reads of the
	// calendar.
	ReadInterval time.Duration
	// Calendars holds the IDs of the calendars to read. Empty means the main
	// calendar of the user.
	Calendars []string
}

// Alarm holds the alarm settings and how the alarm sound plays.
type Alarm struct {
	// SoundFile is the path of the alarm sound, an MP3 or WAV file.
	SoundFile string
	// Length is how long the alarm sound plays.
	Length time.Duration
	// Volume is the speaker volume, from 0 to 100.
	Volume int
	// Weekdays holds the alarm setting for each weekday. A weekday that the
	// file leaves out is off.
	Weekdays [7]AlarmSetting
	// Dates holds the alarm settings for single dates. A date setting wins
	// over the weekday setting, so an off date setting is kept here too.
	Dates map[Date]AlarmSetting
}

// AlarmSetting is the alarm setting of one day: a time of day, or off.
type AlarmSetting struct {
	// On is false when the setting is "none".
	On     bool
	Hour   int
	Minute int
}

// Date is a calendar date without a time zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// String returns the date in the form 2006-01-02.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

type fileSettings struct {
	Alarm    fileAlarm    `toml:"alarm"`
	Calendar fileCalendar `toml:"calendar"`
}

type fileCalendar struct {
	LeadTime     *string  `toml:"lead_time"`
	ReadInterval *string  `toml:"read_interval"`
	Calendars    []string `toml:"calendars"`
}

type fileAlarm struct {
	SoundFile string            `toml:"sound_file"`
	Length    *string           `toml:"length"`
	Volume    *int              `toml:"volume"`
	Weekdays  map[string]string `toml:"weekdays"`
	Dates     map[string]string `toml:"dates"`
}

var weekdayKeys = map[string]time.Weekday{
	"sunday":    time.Sunday,
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
}

// Load reads and checks the settings file at path. A relative sound_file is
// relative to the directory of the settings file. The sound file must be a
// regular file, so a wrong path is found when the settings file is read.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("read settings file: %w", err)
	}
	s, err := Parse(data)
	if err != nil {
		return Settings{}, err
	}
	if !filepath.IsAbs(s.Alarm.SoundFile) {
		s.Alarm.SoundFile = filepath.Join(filepath.Dir(path), s.Alarm.SoundFile)
	}
	info, err := os.Stat(s.Alarm.SoundFile)
	if err != nil {
		return Settings{}, fmt.Errorf("alarm.sound_file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Settings{}, fmt.Errorf("alarm.sound_file: %q is not a regular file", s.Alarm.SoundFile)
	}
	return s, nil
}

// Parse checks the contents of a settings file. Each error names the key
// that is wrong.
func Parse(data []byte) (Settings, error) {
	var f fileSettings
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Settings{}, fmt.Errorf("parse settings file: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Settings{}, fmt.Errorf("unknown key %s", undecoded[0])
	}

	a := Alarm{Length: DefaultLength, Dates: map[Date]AlarmSetting{}}

	ext := strings.ToLower(filepath.Ext(f.Alarm.SoundFile))
	if ext != ".mp3" && ext != ".wav" {
		return Settings{}, fmt.Errorf("alarm.sound_file: %q is not an .mp3 or .wav file", f.Alarm.SoundFile)
	}
	a.SoundFile = f.Alarm.SoundFile

	if f.Alarm.Length != nil {
		d, err := time.ParseDuration(*f.Alarm.Length)
		if err != nil {
			return Settings{}, fmt.Errorf("alarm.length: %w", err)
		}
		if d <= 0 {
			return Settings{}, fmt.Errorf("alarm.length: %s is not more than 0", d)
		}
		a.Length = d
	}

	if f.Alarm.Volume == nil {
		return Settings{}, errors.New("alarm.volume: missing")
	}
	if v := *f.Alarm.Volume; v < 0 || v > 100 {
		return Settings{}, fmt.Errorf("alarm.volume: %d is not from 0 to 100", v)
	}
	a.Volume = *f.Alarm.Volume

	for key, value := range f.Alarm.Weekdays {
		day, ok := weekdayKeys[key]
		if !ok {
			return Settings{}, fmt.Errorf("alarm.weekdays.%s: unknown weekday", key)
		}
		setting, err := parseAlarmSetting(value)
		if err != nil {
			return Settings{}, fmt.Errorf("alarm.weekdays.%s: %w", key, err)
		}
		a.Weekdays[day] = setting
	}

	for key, value := range f.Alarm.Dates {
		t, err := time.Parse(time.DateOnly, key)
		if err != nil {
			return Settings{}, fmt.Errorf("alarm.dates.%q: date is not in the form 2006-01-02", key)
		}
		setting, err := parseAlarmSetting(value)
		if err != nil {
			return Settings{}, fmt.Errorf("alarm.dates.%q: %w", key, err)
		}
		a.Dates[Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}] = setting
	}

	c := Calendar{LeadTime: DefaultLeadTime, ReadInterval: DefaultReadInterval}
	if f.Calendar.LeadTime != nil {
		d, err := time.ParseDuration(*f.Calendar.LeadTime)
		if err != nil {
			return Settings{}, fmt.Errorf("calendar.lead_time: %w", err)
		}
		if d <= 0 {
			return Settings{}, fmt.Errorf("calendar.lead_time: %s is not more than 0", d)
		}
		c.LeadTime = d
	}
	if f.Calendar.ReadInterval != nil {
		d, err := time.ParseDuration(*f.Calendar.ReadInterval)
		if err != nil {
			return Settings{}, fmt.Errorf("calendar.read_interval: %w", err)
		}
		if d <= 0 {
			return Settings{}, fmt.Errorf("calendar.read_interval: %s is not more than 0", d)
		}
		c.ReadInterval = d
	}
	for i, id := range f.Calendar.Calendars {
		if id == "" {
			return Settings{}, fmt.Errorf("calendar.calendars[%d]: empty calendar ID", i)
		}
	}
	// A copy, so that a caller that changes its list leaves the settings as
	// they are.
	c.Calendars = slices.Clone(f.Calendar.Calendars)

	return Settings{Alarm: a, Calendar: c}, nil
}

// parseAlarmSetting reads "HH:MM" or "none".
func parseAlarmSetting(value string) (AlarmSetting, error) {
	if value == "none" {
		return AlarmSetting{}, nil
	}
	bad := fmt.Errorf("%q is not \"HH:MM\" or \"none\"", value)
	if len(value) != 5 || value[2] != ':' {
		return AlarmSetting{}, bad
	}
	hour, ok1 := twoDigits(value[0:2])
	minute, ok2 := twoDigits(value[3:5])
	if !ok1 || !ok2 || hour > 23 || minute > 59 {
		return AlarmSetting{}, bad
	}
	return AlarmSetting{On: true, Hour: hour, Minute: minute}, nil
}

func twoDigits(s string) (int, bool) {
	if s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return 0, false
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), true
}
