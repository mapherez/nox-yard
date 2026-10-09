// Package schedule owns daily wall-clock occurrences. Docker replacement stays
// in the existing managed/Engine workers; scheduling never replays a job.
package schedule

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
)

func Location(name string) (*time.Location, error) { return time.LoadLocation(name) }

var ErrInvalidTime = errors.New("Choose a daily check time in HH:MM format (00:00 to 23:59).")

func ParseTime(value string) (int, int, error) {
	at, err := time.Parse("15:04", value)
	if err != nil || at.Format("15:04") != value {
		return 0, 0, ErrInvalidTime
	}
	return at.Hour(), at.Minute(), nil
}
func Slot(now time.Time, location *time.Location) time.Time {
	return SlotAt(now, location, 3, 0)
}
func dailyAt(year int, month time.Month, day, hour, minute int, location *time.Location) time.Time {
	slot := time.Date(year, month, day, hour, minute, 0, 0, location)
	want := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	wall := time.Date(slot.Year(), slot.Month(), slot.Day(), slot.Hour(), slot.Minute(), 0, 0, time.UTC)
	// A nonexistent spring time moves forward by the DST gap.
	if wall.Before(want) {
		slot = slot.Add(want.Sub(wall))
	}
	// Choose the first occurrence when the clock repeats a wall time.
	_, offset := slot.Zone()
	_, previousOffset := slot.Add(-24 * time.Hour).Zone()
	earlier := slot.Add(time.Duration(offset-previousOffset) * time.Second)
	if earlier.Before(slot) && earlier.Year() == year && earlier.Month() == month && earlier.Day() == day && earlier.Hour() == hour && earlier.Minute() == minute {
		slot = earlier
	}
	return slot
}
func SlotAt(now time.Time, location *time.Location, hour, minute int) time.Time {
	local := now.In(location)
	slot := dailyAt(local.Year(), local.Month(), local.Day(), hour, minute, location)
	if now.Before(slot) {
		y, m, d := local.AddDate(0, 0, -1).Date()
		slot = dailyAt(y, m, d, hour, minute, location)
	}
	return slot
}
func Next(now time.Time, location *time.Location) time.Time {
	return NextAt(now, location, 3, 0)
}
func NextAt(now time.Time, location *time.Location, hour, minute int) time.Time {
	local := now.In(location)
	slot := dailyAt(local.Year(), local.Month(), local.Day(), hour, minute, location)
	if !slot.After(now) {
		y, m, d := local.AddDate(0, 0, 1).Date()
		slot = dailyAt(y, m, d, hour, minute, location)
	}
	return slot
}

type Manager struct {
	Data      *store.Store
	Location  *time.Location
	Run       func(context.Context, store.ProjectSchedule, store.ScheduleOccurrence) error
	Notify    func()
	Reconcile func(context.Context)
	mu        sync.Mutex
}

func (m *Manager) Start(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			if err := m.Tick(ctx, time.Now()); err != nil && ctx.Err() == nil {
				log.Print("project scheduler could not finish its check")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}()
	return func() { cancel(); <-done }
}
func (m *Manager) Tick(ctx context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Reconcile != nil {
		m.Reconcile(ctx)
	}
	busy, err := m.Data.AutomaticBusy(ctx)
	if err != nil {
		return err
	}
	if busy {
		return m.deferDue(ctx, now)
	}
	due, err := m.Data.DueSchedules(ctx, now.Unix())
	if err != nil {
		return err
	}
	for _, row := range due {
		hour, minute, err := ParseTime(row.Time)
		if err != nil {
			return err
		}
		slot := SlotAt(now, m.Location, hour, minute)
		// A persisted check may be later than today's civil slot after a timezone
		// change. It is already due, so do not reject it using the earlier slot.
		dueAt := max(slot.Unix(), row.NextAt)
		occurrence := store.ScheduleOccurrence{Key: row.Key, DueAt: dueAt, Date: slot.In(m.Location).Format("2006-01-02"), NextAt: NextAt(now, m.Location, hour, minute).Unix()}
		attempt, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = m.Run(attempt, row, occurrence)
		cancel()
		if err != nil && !errors.Is(err, store.ErrScheduleChanged) {
			return err
		}
		if errors.Is(err, store.ErrScheduleChanged) {
			if err := m.Data.CoalesceOccurrence(ctx, row.Key, occurrence.Date, occurrence.NextAt); err != nil {
				return err
			}
		}
		if m.Notify != nil {
			m.Notify()
		}
		busy, err = m.Data.AutomaticBusy(ctx)
		if err != nil {
			return err
		}
		if busy {
			return m.deferDue(ctx, now)
		}
	}
	return nil
}

func (m *Manager) deferDue(ctx context.Context, now time.Time) error {
	if err := m.Data.DeferDueSchedules(ctx, now.Unix()); err != nil {
		return err
	}
	if m.Notify != nil {
		m.Notify()
	}
	return nil
}
