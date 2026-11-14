package scheduler

import (
	"fmt"
	"time"
)

const catchUpWindow = 6 * time.Hour

var clockFloor = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type DailyTime struct{ Hour, Minute int }

func ParseDailyTime(s string) (DailyTime, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return DailyTime{}, fmt.Errorf("cannot parse daily time %q: %w", s, err)
	}
	return DailyTime{Hour: t.Hour(), Minute: t.Minute()}, nil
}

func (d DailyTime) slot(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), d.Hour, d.Minute, 0, 0, now.Location())
}

// MorningDue expects now in the digest zone. Before today's slot, the slot is yesterday's, so the window crosses midnight.
func MorningDue(now time.Time, at DailyTime, last time.Time) bool {
	slot := at.slot(now)
	if now.Before(slot) {
		slot = at.slot(now.AddDate(0, 0, -1))
	}
	return now.Before(slot.Add(catchUpWindow)) && last.Before(slot)
}

func IntervalDue(now, last time.Time, every time.Duration) bool {
	return last.IsZero() || now.Sub(last) >= every
}

func ClockSane(now time.Time) bool {
	return now.After(clockFloor)
}
