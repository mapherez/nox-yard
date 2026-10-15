package selfupdate

import (
	"context"
	"github.com/mapherez/nox-yard/internal/store"
	"testing"
	"time"
)

func TestDailyMigrationPreservesEnabledAndExistingTime(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			data, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			if err := data.SetSelfUpdateSettings(enabled, 5); err != nil {
				t.Fatal(err)
			}
			m := New(data, "fixture")
			m.Target = "compose:custom-yard"
			location := time.FixedZone("fixture-server", 2*3600)
			if err := m.initializeDaily(context.Background(), location); err != nil {
				t.Fatal(err)
			}
			row, found, err := data.ProjectSchedule(context.Background(), m.Target)
			if err != nil || !found || row.Enabled != enabled || row.Time != "03:00" {
				t.Fatal("daily migration changed enabled state", row, err)
			}
			if enabled {
				if next := time.Unix(row.NextAt, 0).In(location); next.Hour() != 3 || next.Minute() != 0 {
					t.Fatal("wrong server-local daily slot", next)
				}
			} else if row.NextAt != 0 {
				t.Fatal("disabled migration queued work")
			}
			custom, err := data.SetProjectScheduleAt(context.Background(), row.Key, row.TargetID, enabled, time.Now().Add(12*time.Hour).Unix(), "08:45")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.initializeDaily(context.Background(), location); err != nil {
				t.Fatal(err)
			}
			after, _, _ := data.ProjectSchedule(context.Background(), m.Target)
			if after.Time != custom.Time || after.NextAt != custom.NextAt {
				t.Fatal("restart replaced the edited daily schedule")
			}
			if err := m.SetSettings(false, 15); err != nil {
				t.Fatal(err)
			}
			if err := m.SetSettings(true, 5); err != nil {
				t.Fatal(err)
			}
			after, _, _ = data.ProjectSchedule(context.Background(), m.Target)
			next := time.Unix(after.NextAt, 0).In(location)
			if !after.Enabled || !next.After(time.Now()) || next.Hour() != 8 || next.Minute() != 45 {
				t.Fatal("legacy settings did not retain the daily slot", after)
			}
		})
	}
}
