package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/schedule"
	"github.com/mapherez/nox-yard/internal/store"
)

type scheduleReader struct {
	inventory.Reader
	projects []inventory.Project
}

type schedulePreview struct{}

func (schedulePreview) Preview(context.Context, recreate.Request) (recreate.Preview, error) {
	return recreate.Preview{}, nil
}
func (schedulePreview) AutomaticPreview(context.Context, recreate.Request) (recreate.Preview, error) {
	return recreate.Preview{}, nil
}
func (schedulePreview) Submit(context.Context, recreate.Request) (store.Job, error) {
	panic("saving a schedule must not submit an update")
}

func TestScheduleCustomTimeRecalculatesNextAndLegacySavePreservesTime(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	loc, err := schedule.Location("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	app := New(data, inventory.NewNotifier())
	app.Scheduler = &schedule.Manager{Data: data, Location: loc}
	app.Inventory = scheduleReader{projects: []inventory.Project{{ID: "compose:sample", Name: "sample", Kind: "compose"}}}
	app.Recreator = schedulePreview{}
	for _, value := range []struct {
		at           string
		hour, minute int
	}{{"06:45", 6, 45}, {"22:10", 22, 10}} {
		before := time.Now()
		status, err := app.SetProjectScheduleTime(t.Context(), "compose:sample", true, value.at)
		after := time.Now()
		if err != nil || !status.Enabled || status.Time != value.at || status.Timezone != "Europe/Lisbon" || status.NextAt < schedule.NextAt(before, loc, value.hour, value.minute).Unix() || status.NextAt > schedule.NextAt(after, loc, value.hour, value.minute).Unix() {
			t.Fatal("custom time did not recalculate next check", status, err)
		}
	}
	status, err := app.SetProjectSchedule(t.Context(), "compose:sample", false)
	if err != nil || status.Enabled || status.Time != "22:10" || status.NextAt != 0 {
		t.Fatal("legacy disable lost time", status, err)
	}
	status, err = app.SetProjectSchedule(t.Context(), "compose:sample", true)
	if err != nil || !status.Enabled || status.Time != "22:10" {
		t.Fatal("legacy enable lost time", status, err)
	}
}

func (r scheduleReader) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	return inventory.Snapshot{Projects: r.projects}, ctx.Err()
}
func TestScheduleProtectsYardAndAbsentTargetsAndKeepsLineageOnDisable(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	app := New(data, inventory.NewNotifier())
	app.Scheduler = &schedule.Manager{Data: data, Location: time.UTC}
	app.Inventory = scheduleReader{projects: []inventory.Project{{ID: "compose:nox-yard", Name: "nox-yard"}}}
	status, err := app.ProjectSchedule(t.Context(), "compose:nox-yard")
	if err != nil || status.Eligible || status.Enabled {
		t.Fatal(status, err)
	}
	if _, err := app.SetProjectSchedule(t.Context(), status.TargetID, true); !errors.Is(err, ErrScheduleUnsupported) {
		t.Fatal("Yard enabled", err)
	}
	data.SetProjectSchedule(t.Context(), status.TargetID, status.TargetID, true, 100)
	if err := app.RunScheduledUpdate(t.Context(), store.ProjectSchedule{Key: status.TargetID, TargetID: status.TargetID}, store.ScheduleOccurrence{Key: status.TargetID, DueAt: 100, NextAt: 86500, Date: "2026-10-08"}); err != nil {
		t.Fatal(err)
	}
	row, _, _ := data.ProjectSchedule(t.Context(), status.TargetID)
	if row.LastOutcome != "skipped" {
		t.Fatal("protected schedule executed", row)
	}
	root, current := "container:"+strings.Repeat("a", 64), "container:"+strings.Repeat("b", 64)
	data.SetProjectSchedule(t.Context(), root, current, true, 100)
	status, err = app.SetProjectSchedule(t.Context(), root, false)
	if err != nil || status.Enabled || status.TargetID != current {
		t.Fatal("disable rewound lineage", status, err)
	}
	if _, err := app.SetProjectSchedule(t.Context(), "compose:absent", true); err == nil {
		t.Fatal("absent target enabled")
	}
}
