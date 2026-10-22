package alarm

import (
	"testing"
	"time"

	"github.com/mkuri/press-start/internal/core/calendar"
	"github.com/mkuri/press-start/internal/core/settings"
)

func testAlarm() settings.Alarm {
	return settings.Alarm{
		Weekdays: [7]settings.AlarmSetting{
			time.Monday:   {On: true, Hour: 6, Minute: 30},
			time.Tuesday:  {On: true, Hour: 7, Minute: 0},
			time.Saturday: {On: true, Hour: 8, Minute: 5},
		},
		Dates: map[settings.Date]settings.AlarmSetting{
			// A Wednesday, which has no weekday setting.
			{Year: 2026, Month: time.December, Day: 23}: {On: true, Hour: 9, Minute: 15},
			// A Monday.
			{Year: 2026, Month: time.December, Day: 21}: {On: true, Hour: 5, Minute: 45},
			// A Tuesday.
			{Year: 2026, Month: time.December, Day: 22}: {},
		},
	}
}

func TestWakeTime(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*60*60)
	tests := []struct {
		name   string
		date   settings.Date
		wantOK bool
		hour   int
		minute int
	}{
		{"monday weekday setting", settings.Date{Year: 2026, Month: time.December, Day: 14}, true, 6, 30},
		{"tuesday weekday setting", settings.Date{Year: 2026, Month: time.December, Day: 15}, true, 7, 0},
		{"saturday weekday setting", settings.Date{Year: 2026, Month: time.December, Day: 19}, true, 8, 5},
		{"wednesday without setting", settings.Date{Year: 2026, Month: time.December, Day: 16}, false, 0, 0},
		{"sunday without setting", settings.Date{Year: 2026, Month: time.December, Day: 20}, false, 0, 0},
		{"date setting wins over weekday setting", settings.Date{Year: 2026, Month: time.December, Day: 21}, true, 5, 45},
		{"date setting on weekday without setting", settings.Date{Year: 2026, Month: time.December, Day: 23}, true, 9, 15},
		{"date setting of none wins over weekday setting", settings.Date{Year: 2026, Month: time.December, Day: 22}, false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := WakeTime(tt.date, tokyo, testAlarm(), nil, 10*time.Minute, time.Time{})
			ok := reason != ReasonNone
			if ok != tt.wantOK {
				t.Fatalf("WakeTime ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if reason != ReasonAlarmSetting {
				t.Errorf("WakeTime reason = %q, want %q", reason, ReasonAlarmSetting)
			}
			want := time.Date(tt.date.Year, tt.date.Month, tt.date.Day, tt.hour, tt.minute, 0, 0, tokyo)
			if !got.Equal(want) {
				t.Errorf("WakeTime = %v, want %v", got, want)
			}
		})
	}
}

func TestWakeTimeLocation(t *testing.T) {
	date := settings.Date{Year: 2026, Month: time.December, Day: 14}
	for _, loc := range []*time.Location{
		time.FixedZone("JST", 9*60*60),
		time.UTC,
		time.FixedZone("UTC-8", -8*60*60),
	} {
		name := loc.String()
		got, reason := WakeTime(date, loc, testAlarm(), nil, 10*time.Minute, time.Time{})
		if reason == ReasonNone {
			t.Fatalf("%s: WakeTime ok = false, want true", name)
		}
		if got.Location() != loc {
			t.Errorf("%s: location = %v, want %v", name, got.Location(), loc)
		}
		y, m, d := got.Date()
		if (settings.Date{Year: y, Month: m, Day: d}) != date || got.Hour() != 6 || got.Minute() != 30 {
			t.Errorf("%s: WakeTime = %v, want %s 06:30 in %s", name, got, date, name)
		}
	}
}

func TestWakeTimeEvents(t *testing.T) {
	// 2026-12-14 is a Monday with the alarm setting 06:30, and 2026-12-16 a
	// Wednesday without a setting.
	monday := settings.Date{Year: 2026, Month: time.December, Day: 14}
	wednesday := settings.Date{Year: 2026, Month: time.December, Day: 16}
	event := func(day, hour, minute int) calendar.Event {
		return calendar.Event{Start: time.Date(2026, time.December, day, hour, minute, 0, 0, tokyo)}
	}
	allDay := calendar.Event{Start: time.Date(2026, time.December, 14, 0, 0, 0, 0, tokyo), AllDay: true}
	declined := event(14, 5, 0)
	declined.Declined = true
	free := event(14, 5, 0)
	free.Free = true
	early := at(14, 4, 0, 0)
	tests := []struct {
		name       string
		date       settings.Date
		events     []calendar.Event
		leadTime   time.Duration
		now        time.Time
		wantReason Reason
		want       time.Time
	}{
		{"event earlier than alarm setting", monday, []calendar.Event{event(14, 6, 0)}, 10 * time.Minute, early, ReasonFirstEvent, at(14, 5, 50, 0)},
		{"alarm setting earlier than event", monday, []calendar.Event{event(14, 9, 0)}, 10 * time.Minute, early, ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"event minus lead time equal to alarm setting", monday, []calendar.Event{event(14, 6, 40)}, 10 * time.Minute, early, ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"only event", wednesday, []calendar.Event{event(16, 9, 0)}, 10 * time.Minute, early, ReasonFirstEvent, at(16, 8, 50, 0)},
		{"only alarm setting", monday, nil, 10 * time.Minute, early, ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"first of many events", wednesday, []calendar.Event{event(16, 11, 0), event(16, 8, 0), event(16, 9, 0)}, 10 * time.Minute, early, ReasonFirstEvent, at(16, 7, 50, 0)},
		{"changed lead time", monday, []calendar.Event{event(14, 6, 0)}, 45 * time.Minute, early, ReasonFirstEvent, at(14, 5, 15, 0)},
		{"no event and no alarm setting", wednesday, nil, 10 * time.Minute, early, ReasonNone, time.Time{}},
		{"all-day event", monday, []calendar.Event{allDay}, 10 * time.Minute, early, ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"declined event", monday, []calendar.Event{declined}, 10 * time.Minute, early, ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"free event", monday, []calendar.Event{free}, 10 * time.Minute, early, ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"event before 03:00", monday, []calendar.Event{event(14, 2, 59)}, 10 * time.Minute, at(14, 2, 0, 0), ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"event at 03:00", monday, []calendar.Event{event(14, 3, 0)}, 10 * time.Minute, at(14, 2, 0, 0), ReasonFirstEvent, at(14, 2, 50, 0)},
		{"only events not counted and no alarm setting", wednesday, []calendar.Event{allDay, event(16, 1, 0)}, 10 * time.Minute, at(16, 0, 30, 0), ReasonNone, time.Time{}},
		{"event of another date", wednesday, []calendar.Event{event(15, 9, 0), event(17, 9, 0)}, 10 * time.Minute, early, ReasonNone, time.Time{}},
		{"event started one hour before", monday, []calendar.Event{event(14, 6, 0)}, 10 * time.Minute, at(14, 7, 0, 0), ReasonAlarmSetting, at(14, 6, 30, 0)},
		{"event started less than one hour before", monday, []calendar.Event{event(14, 6, 0)}, 10 * time.Minute, at(14, 6, 59, 59), ReasonFirstEvent, at(14, 5, 50, 0)},
		{"old event skipped for a later event", wednesday, []calendar.Event{event(16, 6, 0), event(16, 9, 0)}, 10 * time.Minute, at(16, 7, 0, 0), ReasonFirstEvent, at(16, 8, 50, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := WakeTime(tt.date, tokyo, testAlarm(), tt.events, tt.leadTime, tt.now)
			if reason != tt.wantReason {
				t.Fatalf("WakeTime reason = %q, want %q", reason, tt.wantReason)
			}
			if !got.Equal(tt.want) {
				t.Errorf("WakeTime = %v, want %v", got, tt.want)
			}
		})
	}
}
