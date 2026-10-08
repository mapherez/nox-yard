package managed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

func TestLegacyMissingDirectoryCannotStartRestartOrUpdate(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	if err := data.SaveManagedProject(store.ManagedProject{Name: "legacy", YAML: "services:\n  web:\n    image: alpine:3.23\n    volumes: ['./data:/data']\n", VariablesJSON: "{}", EnvFilesJSON: "{}", SourceKind: "paste"}); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"start", "restart", "update"} {
		job, err := m.Operation("legacy", operation, false)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for {
			current, found, err := m.Job(job.ID)
			if err != nil || !found {
				t.Fatalf("missing job: %v", err)
			}
			if current.Status != "running" {
				if current.Status != "failed" || !strings.Contains(current.Error, "directory is missing") {
					t.Fatalf("unsafe operation passed: %+v", current)
				}
				m.mu.Lock()
				active := m.active["legacy"]
				m.mu.Unlock()
				if !active {
					break
				}
			}
			if time.Now().After(deadline) {
				t.Fatal("job did not finish")
			}
			time.Sleep(time.Millisecond)
		}
	}
}

type previewInventory struct{ inventory.Reader }

func (previewInventory) Snapshot(context.Context) (inventory.Snapshot, error) {
	return inventory.Snapshot{}, nil
}

func TestLegacyMissingDirectoryCannotSyncBeforeSourceOrDockerAccess(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	if err := data.SaveManagedProject(store.ManagedProject{Name: "legacy", SourceKind: "url", SourceURL: "https://example.com/compose.yml", YAML: "services: {}", VariablesJSON: "{}", EnvFilesJSON: "{}"}); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(data, previewInventory{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = m.Preview(t.Context(), Request{Name: "legacy", Mode: "sync", Source: SourceInput{Kind: "url", URL: "https://example.com/compose.yml"}})
	if !errors.Is(err, ErrInvalidSource) || !strings.Contains(err.Error(), "directory is missing") {
		t.Fatalf("unsafe sync accepted: %v", err)
	}
}
