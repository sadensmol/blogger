package scheduler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/scheduler"
)

func TestMorningDue(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Tbilisi")
	require.NoError(t, err)
	at := scheduler.DailyTime{Hour: 10}
	at1000 := time.Date(2026, 11, 14, 10, 0, 0, 0, zone)
	yesterday := at1000.AddDate(0, 0, -1)

	tests := []struct {
		name string
		now  time.Time
		last time.Time
		want bool
	}{
		{name: "before the slot", now: at1000.Add(-time.Minute), want: false},
		{name: "at the slot with no run", now: at1000, want: true},
		{name: "at the slot after yesterday's run", now: at1000, last: yesterday, want: true},
		{name: "after today's run", now: at1000.Add(30 * time.Minute), last: at1000, want: false},
		{name: "last minute of the catch-up window", now: at1000.Add(6*time.Hour - time.Minute), last: yesterday, want: true},
		{name: "after the catch-up window", now: at1000.Add(6 * time.Hour), last: yesterday, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, scheduler.MorningDue(tt.now, at, tt.last))
		})
	}
}

func TestMorningDueAcrossMidnight(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Tbilisi")
	require.NoError(t, err)
	at := scheduler.DailyTime{Hour: 20}
	slot := time.Date(2026, 11, 14, 20, 0, 0, 0, zone)
	nextDay := func(h, m int) time.Time { return time.Date(2026, 11, 15, h, m, 0, 0, zone) }

	tests := []struct {
		name string
		now  time.Time
		last time.Time
		want bool
	}{
		{name: "after midnight with no run", now: nextDay(1, 0), want: true},
		{name: "after midnight with the slot's run", now: nextDay(1, 0), last: slot, want: false},
		{name: "after midnight with the day before's run", now: nextDay(1, 0), last: slot.AddDate(0, 0, -1), want: true},
		{name: "last minute of the window", now: nextDay(1, 59), want: true},
		{name: "after the window", now: nextDay(2, 0), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, scheduler.MorningDue(tt.now, at, tt.last))
		})
	}
}

func TestClockSane(t *testing.T) {
	require.False(t, scheduler.ClockSane(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)))
	require.True(t, scheduler.ClockSane(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)))
}

func TestIntervalDue(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	every := 3 * time.Hour

	require.False(t, scheduler.IntervalDue(now, now.Add(-2*time.Hour-59*time.Minute), every))
	require.True(t, scheduler.IntervalDue(now, now.Add(-3*time.Hour), every))
	require.True(t, scheduler.IntervalDue(now, time.Time{}, every))
}

func TestParseDailyTime(t *testing.T) {
	got, err := scheduler.ParseDailyTime("10:30")
	require.NoError(t, err)
	require.Equal(t, scheduler.DailyTime{Hour: 10, Minute: 30}, got)

	_, err = scheduler.ParseDailyTime("25:00")
	require.Error(t, err)
}
