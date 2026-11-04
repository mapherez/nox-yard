package store

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSuiteBindingOutboxIsDurableAndRemovalIsTargetScoped(t *testing.T) {
	directory := t.TempDir()
	data, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	finish := func(target, operation string, resources []string, targets []SuiteTarget) {
		t.Helper()
		job, err := NewJob(target, "managed", operation, resources)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(struct {
			SuiteTargets []SuiteTarget `json:"suiteTargets"`
		}{targets})
		job.Payload = string(raw)
		if err = data.CreateJob(job); err != nil {
			t.Fatal(err)
		}
		if err = data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	targets := []SuiteTarget{{ProductID: "future-product", Service: "api"}}
	finish("compose:project", "new", nil, targets)
	initial, err := data.SuiteBindings(context.Background())
	if err != nil || len(initial) != 1 {
		t.Fatalf("missing binding: %v %v", initial, err)
	}
	data.Close()
	data, err = Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	operations, err := data.SuiteOperations(context.Background())
	if err != nil || len(operations) == 0 {
		t.Fatal("outbox lost after restart", err)
	}
	finish("container:companion", "remove", []string{"compose:project"}, nil)
	bindings, err := data.SuiteBindings(context.Background())
	if err != nil || bindings[0].State != "pending" {
		t.Fatal("companion removal retired product", bindings, err)
	}
	finish("compose:project", "update", nil, nil)
	bindings, _ = data.SuiteBindings(context.Background())
	if bindings[0].ID != initial[0].ID || bindings[0].State != "pending" {
		t.Fatal("image update retired marker binding")
	}
	finish("compose:project", "remove", nil, nil)
	bindings, _ = data.SuiteBindings(context.Background())
	if bindings[0].State != "removing" {
		t.Fatal("project removal not queued durably")
	}
}

func TestSuiteSourceEditRetiresMarkerWithoutLosingRecovery(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	source := func(product string) {
		t.Helper()
		job, _ := NewJob("compose:project", "managed", "sync", nil)
		raw, _ := json.Marshal(struct {
			SuiteTargets []SuiteTarget `json:"suiteTargets"`
		}{[]SuiteTarget{{ProductID: product, Service: "api"}}})
		job.Payload = string(raw)
		if err := data.CreateJob(job); err != nil {
			t.Fatal(err)
		}
		if err := data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	source("old-product")
	old, err := data.SuiteBindings(context.Background())
	if err != nil || len(old) != 1 {
		t.Fatal("missing initial binding", err)
	}
	old[0].Private = json.RawMessage(`{"keys":{"recovery":"keep"}}`)
	if err := data.SaveSuiteBinding(old[0]); err != nil {
		t.Fatal(err)
	}
	source("replacement-product")
	bindings, _ := data.SuiteBindings(context.Background())
	if len(bindings) != 2 {
		t.Fatal("source replacement lost or duplicated a binding", bindings)
	}
	for _, b := range bindings {
		if b.ProductID == "old-product" && (b.State != "removing" || string(b.Private) != string(old[0].Private)) {
			t.Fatal("old credential recovery was discarded", b)
		}
		if b.ProductID == "replacement-product" && b.State != "pending" {
			t.Fatal("new marker not pending", b)
		}
	}
}
