package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
)

func TestDailyCivilSlotsRespectLisbonDST(t *testing.T) {
	loc, err := Location("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		day   string
		hours int
	}{{"2026-03-28", 23}, {"2026-10-24", 25}, {"2026-10-08", 24}} {
		now, err := time.ParseInLocation("2006-01-02 15:04", test.day+" 03:00", loc)
		if err != nil {
			t.Fatal(err)
		}
		next := Next(now, loc)
		if next.Sub(now) != time.Duration(test.hours)*time.Hour || next.In(loc).Hour() != 3 {
			t.Fatal("slot used elapsed 24h", now, next)
		}
		if !Slot(now, loc).Equal(now) || !Slot(next.Add(-time.Minute), loc).Equal(now) {
			t.Fatal("wrong latest slot")
		}
	}
	if _, err := Location("browser/timezone"); err == nil {
		t.Fatal("invalid timezone accepted")
	}
}
func TestRestartCoalescesMissesAndCivilDateNeverRepeats(t *testing.T) {
	dir := t.TempDir()
	data, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC)
	key := "compose:sample"
	data.SetProjectSchedule(t.Context(), key, key, true, now.AddDate(0, 0, -5).Unix())
	run := func(ctx context.Context, row store.ProjectSchedule, occ store.ScheduleOccurrence) error {
		return data.SkipSchedule(ctx, row, occ, "No changes.")
	}
	m := &Manager{Data: data, Location: time.UTC, Run: run}
	if err := m.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	data.Close()
	data, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	m.Data = data
	for _, at := range []time.Time{now, now.Add(-time.Hour), now.Add(-48 * time.Hour), now.Add(time.Hour)} {
		if err := m.Tick(t.Context(), at); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ := data.Jobs(t.Context(), key, false)
	if len(jobs) != 1 || jobs[0].ScheduledFor != Slot(now, time.UTC).Unix() {
		t.Fatal("misses replayed", jobs)
	}
	data.SetProjectSchedule(t.Context(), key, key, false, 0)
	data.SetProjectSchedule(t.Context(), key, key, true, now.Unix())
	if err := m.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	jobs, _ = data.Jobs(t.Context(), key, false)
	row, _, _ := data.ProjectSchedule(t.Context(), key)
	if len(jobs) != 1 || row.NextAt != Next(now, time.UTC).Unix() {
		t.Fatal("consumed date repeated or stuck due", row, jobs)
	}
	if err := m.Tick(t.Context(), now.AddDate(0, 0, 3)); err != nil {
		t.Fatal(err)
	}
	jobs, _ = data.Jobs(t.Context(), key, false)
	if len(jobs) != 2 {
		t.Fatal("multiple catchup jobs", jobs)
	}
}
func TestAutomaticGlobalReservationDefersOtherProjects(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	now := time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC)
	for _, key := range []string{"compose:a", "compose:b", "compose:off"} {
		data.SetProjectSchedule(t.Context(), key, key, key != "compose:off", now.Add(-time.Hour).Unix())
	}
	m := &Manager{Data: data, Location: time.UTC, Run: func(ctx context.Context, row store.ProjectSchedule, occ store.ScheduleOccurrence) error {
		job, _ := store.NewJob(row.TargetID, "managed", "update", nil)
		return data.CreateJob(store.ScheduledJob(store.WithSchedule(ctx, occ), job))
	}}
	if err := m.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	jobs, _ := data.Jobs(t.Context(), "", false)
	if len(jobs) != 1 {
		t.Fatal("more than one worker", jobs)
	}
	row, _, _ := data.ProjectSchedule(t.Context(), "compose:b")
	if row.LastOutcome != "deferred" || row.NextAt > now.Unix() {
		t.Fatal("deferral consumed occurrence", row)
	}
	data.FinishJob(jobs[0].ID, jobs[0].Owner, "succeeded", "unchanged", "", "")
	if err := m.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	jobs, _ = data.Jobs(t.Context(), "", false)
	if len(jobs) != 2 {
		t.Fatal("deferred project did not catch up", jobs)
	}
}
