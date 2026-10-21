// Package alarm decides when press-start wakes the user.
package alarm

import (
	"time"

	"github.com/mkuri/press-start/internal/core/settings"
)

// WakeTime returns the wake time of date in loc from the alarm settings. The
// second result is false when the day has no wake-up run. A date setting wins
// over the weekday setting, also when the date setting is off. Otherwise the
// weekday setting of date is used.
func WakeTime(date settings.Date, loc *time.Location, a settings.Alarm) (time.Time, bool) {
	setting, ok := a.Dates[date]
	if !ok {
		weekday := time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC).Weekday()
		setting = a.Weekdays[weekday]
	}
	if !setting.On {
		return time.Time{}, false
	}
	return time.Date(date.Year, date.Month, date.Day, setting.Hour, setting.Minute, 0, 0, loc), true
}
