package alarm

import (
	"testing"
	"time"

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
			got, ok := WakeTime(tt.date, tokyo, testAlarm())
			if ok != tt.wantOK {
				t.Fatalf("WakeTime ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
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
		got, ok := WakeTime(date, loc, testAlarm())
		if !ok {
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
