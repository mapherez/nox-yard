package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/schedule"
	"github.com/mapherez/nox-yard/internal/store"
)

type scheduleReader struct {
	inventory.Reader
	projects []inventory.Project
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
