// Package calendar holds the calendar events that decide the wake time, in a
// form that does not depend on the calendar provider.
package calendar

import (
	"context"
	"time"
)

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
	// Events returns the events that start from from until before to.
	Events(ctx context.Context, from, to time.Time) ([]Event, error)
}
