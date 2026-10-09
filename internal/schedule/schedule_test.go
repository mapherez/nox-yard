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

func TestDueScheduleSurvivesTimezoneChange(t *testing.T) {
	loc, err := Location("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	key := "compose:nginx-proxy-manager"
	// A schedule saved at 03:00 UTC appears as 04:00 in Lisbon on this date.
	enabledAt := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	due := Next(enabledAt, time.UTC)
	if _, err := data.SetProjectSchedule(t.Context(), key, key, true, due.Unix()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 15, 53, 0, 0, loc)
	m := &Manager{Data: data, Location: loc, Run: func(ctx context.Context, row store.ProjectSchedule, occ store.ScheduleOccurrence) error {
		return data.SkipSchedule(ctx, row, occ, "No image changes.")
	}}
	for _, at := range []time.Time{now, now.Add(time.Minute)} {
		if err := m.Tick(t.Context(), at); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := data.Jobs(t.Context(), key, false)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("overdue schedule was not consumed exactly once: jobs=%v error=%v", jobs, err)
	}
	row, _, err := data.ProjectSchedule(t.Context(), key)
	wantNext := time.Date(2026, 10, 10, 3, 0, 0, 0, loc)
	if err != nil || row.LastOutcome != "skipped" || row.NextAt != wantNext.Unix() || jobs[0].ScheduledFor != due.Unix() {
		t.Fatalf("schedule did not recover in the configured timezone: row=%+v jobs=%v error=%v", row, jobs, err)
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

func TestProjectsUseTheirOwnDailyTimes(t *testing.T) {
	loc, err := Location("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	now := time.Date(2026, 10, 9, 6, 44, 0, 0, loc)
	for _, value := range []struct {
		key, at      string
		hour, minute int
	}{{"compose:early", "06:45", 6, 45}, {"compose:late", "22:10", 22, 10}} {
		next := NextAt(now, loc, value.hour, value.minute)
		if _, err := data.SetProjectScheduleAt(t.Context(), value.key, value.key, true, next.Unix(), value.at); err != nil {
			t.Fatal(err)
		}
	}
	m := &Manager{Data: data, Location: loc, Run: func(ctx context.Context, row store.ProjectSchedule, occ store.ScheduleOccurrence) error {
		return data.SkipSchedule(ctx, row, occ, "No image changes.")
	}}
	for _, value := range []struct{ hour, minute, count int }{{6, 44, 0}, {6, 45, 1}, {22, 9, 1}, {22, 10, 2}, {23, 59, 2}} {
		at := time.Date(2026, 10, 9, value.hour, value.minute, 0, 0, loc)
		if err := m.Tick(t.Context(), at); err != nil {
			t.Fatal(err)
		}
		jobs, err := data.Jobs(t.Context(), "", false)
		if err != nil || len(jobs) != value.count {
			t.Fatalf("at %v: jobs=%v error=%v", at, jobs, err)
		}
	}
	row, _, _ := data.ProjectSchedule(t.Context(), "compose:early")
	if row.NextAt != time.Date(2026, 10, 10, 6, 45, 0, 0, loc).Unix() {
		t.Fatal("wrong next custom occurrence", row)
	}
	changedAt := time.Date(2026, 10, 9, 23, 59, 0, 0, loc)
	if _, err := data.SetProjectScheduleAt(t.Context(), row.Key, row.TargetID, true, changedAt.Unix(), "23:59"); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(t.Context(), changedAt); err != nil {
		t.Fatal(err)
	}
	jobs, err := data.Jobs(t.Context(), "", false)
	row, _, _ = data.ProjectSchedule(t.Context(), row.Key)
	if err != nil || len(jobs) != 2 || row.NextAt != time.Date(2026, 10, 10, 23, 59, 0, 0, loc).Unix() {
		t.Fatal("time change repeated today's check", row, jobs, err)
	}
}

func TestDailyTimeValidation(t *testing.T) {
	for _, at := range []string{"00:00", "03:00", "06:45", "23:59"} {
		if _, _, err := ParseTime(at); err != nil {
			t.Fatal(at, err)
		}
	}
	for _, at := range []string{"", "3:00", "24:00", "12:60", "03:00:00", " 03:00", "03:00Z"} {
		if _, _, err := ParseTime(at); err == nil {
			t.Fatal("invalid time accepted", at)
		}
	}
}

func TestCustomTimeAcrossLisbonClockChanges(t *testing.T) {
	loc, err := Location("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct{ day, want string }{{"2026-03-29", "2026-03-29T01:30:00Z"}, {"2026-10-25", "2026-10-25T00:30:00Z"}} {
		now, err := time.ParseInLocation("2006-01-02 15:04", value.day+" 00:00", loc)
		if err != nil {
			t.Fatal(err)
		}
		next := NextAt(now, loc, 1, 30)
		if next.UTC().Format(time.RFC3339) != value.want {
			t.Fatal("wrong gap/repeated time", next, value.want)
		}
		if !SlotAt(next, loc, 1, 30).Equal(next) {
			t.Fatal("slot did not match next", next)
		}
		if NextAt(next, loc, 1, 30).In(loc).Day() == next.In(loc).Day() {
			t.Fatal("repeated wall time scheduled twice", next)
		}
	}
}
