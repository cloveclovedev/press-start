// Package calendar holds the calendar events that decide the wake time, in a
// form that does not depend on the calendar provider.
package calendar

import (
	"context"
	"errors"
	"time"
)

// ErrTokenExpired tells that the calendar refuses the credentials, so the
// user must authorize press-start again. A quick retry does not help, so a
// Source wraps this error for such a failure.
var ErrTokenExpired = errors.New("calendar: token expired or revoked")

// Event is one calendar event.
type Event struct {
	// Start is when the event starts. For an all-day event, it is the start
	// of its first day.
	Start time.Time
	// AllDay is true for an event without a start time of day.
	AllDay bool
	// Declined is true when the user declined the event.
	Declined bool
	// Free is true when the event shows the user as free.
	Free bool
}

// Source reads the events from a calendar. A calendar adapter implements it.
type Source interface {
	// Events returns the events of the calendars with the IDs in calendars
	// that start from from until before to. An empty calendars means the
	// main calendar of the user.
	Events(ctx context.Context, calendars []string, from, to time.Time) ([]Event, error)
}
