// Package alarm decides when press-start wakes the user.
package alarm

import (
	"time"

	"github.com/mkuri/press-start/internal/core/calendar"
	"github.com/mkuri/press-start/internal/core/settings"
)

// Reason tells what decided the wake time.
type Reason string

// The reasons of a wake time.
const (
	// ReasonAlarmSetting is a wake time from the alarm setting of the day.
	ReasonAlarmSetting Reason = "alarm setting"
	// ReasonFirstEvent is a wake time from the first event minus the lead
	// time.
	ReasonFirstEvent Reason = "first event"
	// ReasonNone is a day without a wake-up run.
	ReasonNone Reason = "none"
)

// countedFrom is the time of day from which an event counts. An earlier
// event goes on from the night before and is not one to wake up for.
const countedFrom = 3 * time.Hour

// staleAfter is how long after its start an event stops counting. Ringing
// for an event found that late does not help.
const staleAfter = time.Hour

// WakeTime returns the wake time of date in loc, decided at now. It is the
// earlier of the alarm setting and the start of the first counted event
// minus leadTime. With only one of them, it is that one. The reason is
// ReasonNone, with a zero time, when the day has no wake-up run.
//
// A date setting wins over the weekday setting, also when the date setting
// is off. Otherwise the weekday setting of date is used.
//
// An event counts when it starts on date at 03:00 or later, and is not
// all-day, declined, or free. An event that started one hour or more before
// now does not count.
func WakeTime(date settings.Date, loc *time.Location, a settings.Alarm, events []calendar.Event, leadTime time.Duration, now time.Time) (time.Time, Reason) {
	wake, reason := time.Time{}, ReasonNone
	setting, ok := a.Dates[date]
	if !ok {
		weekday := time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC).Weekday()
		setting = a.Weekdays[weekday]
	}
	if setting.On {
		wake, reason = time.Date(date.Year, date.Month, date.Day, setting.Hour, setting.Minute, 0, 0, loc), ReasonAlarmSetting
	}

	if first, ok := firstEvent(date, loc, events, now); ok {
		if fromEvent := first.Add(-leadTime); reason == ReasonNone || fromEvent.Before(wake) {
			wake, reason = fromEvent, ReasonFirstEvent
		}
	}
	return wake, reason
}

// firstEvent returns the start of the earliest counted event of date.
func firstEvent(date settings.Date, loc *time.Location, events []calendar.Event, now time.Time) (time.Time, bool) {
	dayStart := time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, loc)
	from := dayStart.Add(countedFrom)
	to := time.Date(date.Year, date.Month, date.Day+1, 0, 0, 0, 0, loc)
	var first time.Time
	found := false
	for _, e := range events {
		if e.AllDay || e.Declined || e.Free {
			continue
		}
		if e.Start.Before(from) || !e.Start.Before(to) {
			continue
		}
		if now.Sub(e.Start) >= staleAfter {
			continue
		}
		if !found || e.Start.Before(first) {
			first, found = e.Start, true
		}
	}
	return first, found
}
