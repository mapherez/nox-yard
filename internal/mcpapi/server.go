// Package mcpapi adapts the shared Yard facade to native NoX MCP tools.
package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
)

const Timeout = 60 * time.Second
const MaxPayloadBytes = 8 * 1024 * 1024
const MaxInFlight = 32

type flags struct{ readOnly, destructive, idempotent bool }

var read = flags{true, false, true}
var write = flags{false, true, false}
var setting = flags{false, true, true}

type builder struct {
	tools []noxmcp.Tool
	err   error
}

// The application remains unaware of MCP schemas and protocol error envelopes.
func register[I, O any](b *builder, name, title, description string, f flags, execute func(context.Context, I) (O, error)) {
	if b.err != nil {
		return
	}
	input, err := jsonschema.For[I](nil)
	if err != nil {
		b.err = fmt.Errorf("%s input: %w", name, err)
		return
	}
	output, err := jsonschema.For[O](nil)
	if err != nil {
		b.err = fmt.Errorf("%s output: %w", name, err)
		return
	}
	b.tools = append(b.tools, noxmcp.Tool{Name: name, Title: title, Description: description, InputSchema: input, OutputSchema: output, ReadOnly: f.readOnly, Destructive: f.destructive, Idempotent: f.idempotent,
		Execute: func(ctx noxmcp.ExecutionContext, args map[string]any) (map[string]any, error) {
			data, err := json.Marshal(args)
			if err != nil {
				return nil, err
			}
			var in I
			if err = json.Unmarshal(data, &in); err != nil {
				return nil, &noxmcp.Error{Code: "INVALID_INPUT", Message: "Invalid input"}
			}
			value, err := execute(ctx.Context, in)
			if err != nil {
				var public *noxmcp.Error
				if errors.As(err, &public) {
					return nil, public
				}
				_, p := application.ClassifyError(err, false)
				return nil, toolError(p, nil)
			}
			return object(value)
		}})
}
func object(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}
func toolError(p application.Problem, result any) *noxmcp.Error {
	e := &noxmcp.Error{Code: p.Code, Message: p.Message}
	if result != nil {
		e.Details = map[string]any{"result": result}
	}
	return e
}
func target(id string, container bool) error {
	if !application.ValidTarget(id, container) {
		return &noxmcp.Error{Code: "INVALID_TARGET_ID", Message: "Invalid target ID."}
	}
	return nil
}

type empty struct{}
type targetInput struct {
	ID string `json:"id"`
}
type actionInput struct {
	ID     string           `json:"id"`
	Action lifecycle.Action `json:"action"`
}
type removalInput struct {
	ID          string `json:"id"`
	Confirm     bool   `json:"confirm"`
	Fingerprint string `json:"fingerprint"`
}
type composeOperation struct {
	Name          string `json:"name"`
	Operation     string `json:"operation"`
	RemoveVolumes bool   `json:"removeVolumes"`
}
type settingsInput struct {
	ProjectsBase string `json:"projectsBase"`
}
type updateSettings struct {
	Automatic       bool `json:"automatic"`
	IntervalMinutes int  `json:"intervalMinutes"`
}
type metricsOutput struct {
	Metrics map[string]inventory.Metrics `json:"metrics"`
}
type logsOutput struct {
	Lines []inventory.LogLine `json:"lines"`
}
type healthOutput struct {
	Service string                   `json:"service"`
	Version string                   `json:"version"`
	Ready   bool                     `json:"ready"`
	Storage application.Availability `json:"storage"`
}
type failure struct {
	Target  string `json:"target"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type actionOutput struct {
	Action    lifecycle.Action `json:"action"`
	Succeeded int              `json:"succeeded"`
	Skipped   int              `json:"skipped"`
	Failed    int              `json:"failed"`
	Queued    int              `json:"queued"`
	Failures  []failure        `json:"failures"`
}
type pullOutput struct {
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Images    []string  `json:"images"`
	Failures  []failure `json:"failures"`
}

func failures(items []lifecycle.Failure) []failure {
	result := make([]failure, 0, len(items))
	for _, item := range items {
		_, p := application.ClassifyError(item.Cause, true)
		result = append(result, failure{item.Target, p.Code, p.Message})
	}
	return result
}
func New(app *application.Service, version string) (*noxmcp.Runtime, error) {
	b := &builder{}
	register(b, "yard_health", "Yard health", "Read storage readiness and application version.", read, func(ctx context.Context, _ empty) (healthOutput, error) {
		result := healthOutput{Service: "nox-yard", Version: version, Ready: true, Storage: application.Availability{Available: true}}
		if err := app.Health(ctx); err != nil {
			result.Ready = false
			result.Storage.Available = false
			return result, toolError(application.Problem{Code: "STORAGE_UNAVAILABLE", Message: "Storage is unavailable."}, result)
		}
		return result, nil
	})
	register(b, "yard_status", "Yard status", "Read Docker availability, version and current inventory counts without additional Docker probes.", read, func(ctx context.Context, _ empty) (application.Status, error) { return app.Status(ctx, version) })
	register(b, "yard_projects", "Yard projects", "Read projects and nested containers, including stored managed projects, pending states and cached metrics. IDs are opaque; use the returned full IDs.", read, func(ctx context.Context, _ empty) (inventory.Snapshot, error) { return app.Projects(ctx) })
	register(b, "yard_metrics", "Yard metrics", "Read cached metrics without querying Docker.", read, func(ctx context.Context, _ empty) (metricsOutput, error) {
		m, err := app.Metrics(ctx)
		return metricsOutput{m}, err
	})
	for _, reveal := range []bool{false, true} {
		name, title, description := "yard_container_inspect", "Inspect container", "Read ports, mounts, networks and environment names. Environment values are not returned."
		if reveal {
			name, title, description = "yard_container_environment", "Read container environment values", "Explicitly read potentially sensitive environment values from this container. Normal inventory and inspection calls do not reveal values."
		}
		register(b, name, title, description, read, func(ctx context.Context, in targetInput) (inventory.ContainerInspection, error) {
			if err := target(in.ID, true); err != nil {
				return inventory.ContainerInspection{}, err
			}
			return app.Inspect(ctx, in.ID, reveal)
		})
	}
	register(b, "yard_container_logs", "Container log snapshot", "Read the last 20 decoded container log records, without following the log stream.", read, func(ctx context.Context, in targetInput) (logsOutput, error) {
		if err := target(in.ID, true); err != nil {
			return logsOutput{}, err
		}
		lines, err := app.LogsSnapshot(ctx, in.ID)
		return logsOutput{lines}, err
	})
	for _, container := range []bool{true, false} {
		kind := "project"
		if container {
			kind = "container"
		}
		register(b, "yard_"+kind+"_action", "Engine "+kind+" action", "Start, stop or restart existing containers through the shared Engine lifecycle implementation. This is distinct from stored Compose operations; restarting Yard may queue its restart helper.", write, func(ctx context.Context, in actionInput) (actionOutput, error) {
			if err := target(in.ID, container); err != nil {
				return actionOutput{}, err
			}
			result, err := app.Action(ctx, in.ID, container, in.Action)
			out := actionOutput{result.Action, result.Succeeded, result.Skipped, result.Failed, result.Queued, failures(result.Failures)}
			_, p := application.OperationProblem(ctx, err, result.Failed, result.Succeeded, result.Skipped, result.Queued, result.Failures)
			if p != nil {
				return out, toolError(*p, out)
			}
			return out, nil
		})
		register(b, "yard_"+kind+"_pull", "Pull "+kind+" images", "Pull configured images into Docker's local cache, without replacing containers. Synchronous, bounded by the 60-second MCP timeout; reconcile state after timeout before retrying.", write, func(ctx context.Context, in targetInput) (pullOutput, error) {
			if err := target(in.ID, container); err != nil {
				return pullOutput{}, err
			}
			result, err := app.Pull(ctx, in.ID, container)
			out := pullOutput{result.Succeeded, result.Failed, append([]string{}, result.Images...), failures(result.Failures)}
			_, p := application.OperationProblem(ctx, err, result.Failed, result.Succeeded, 0, 0, result.Failures)
			if p != nil {
				return out, toolError(*p, out)
			}
			return out, nil
		})
		register(b, "yard_"+kind+"_remove_preview", "Preview "+kind+" removal", "Read the current removal plan and fingerprint. This call does not remove resources.", read, func(ctx context.Context, in targetInput) (lifecycle.RemovalPlan, error) {
			if err := target(in.ID, container); err != nil {
				return lifecycle.RemovalPlan{}, err
			}
			return app.PreviewRemove(ctx, in.ID, container)
		})
		register(b, "yard_"+kind+"_remove", "Remove "+kind, "Remove resources using a confirmed current removal preview and fingerprint. Existing protection and changed-plan checks apply; shared resources and bind data retain their existing treatment.", write, func(ctx context.Context, in removalInput) (lifecycle.RemovalReport, error) {
			if err := target(in.ID, container); err != nil {
				return lifecycle.RemovalReport{}, err
			}
			out, err := app.Remove(ctx, in.ID, container, in.Confirm, in.Fingerprint)
			if err != nil {
				_, p := application.ClassifyError(err, true)
				return out, toolError(p, out)
			}
			for _, item := range out.Items {
				if item.Status == "failed" {
					return out, toolError(application.Problem{Code: "OPERATION_PARTIAL_FAILURE", Message: "Removal completed only partially."}, out)
				}
			}
			return out, nil
		})
	}
	register(b, "yard_compose_source", "Prepare Compose source", "Read and inspect a pasted, uploaded or public HTTPS Compose source, including interpolation variables and environment file requirements.", read, app.PrepareSource)
	register(b, "yard_compose_preview", "Preview Compose project", "Validate the existing new, copy, sync or adopt request and return its preview/fingerprint. No deployment is submitted.", read, app.ComposePreview)
	register(b, "yard_compose_submit", "Submit Compose project", "Submit a previously reviewed Compose request with its current fingerprint. May create, synchronize or adopt a project. Returns a job; query yard_compose_job for completion.", write, app.ComposeSubmit)
	register(b, "yard_compose_operation", "Run Compose operation", "Submit start, stop, restart, pull, update or remove for a stored managed project. removeVolumes controls existing Compose volume removal. Returns a job, not completed work.", write, func(ctx context.Context, in composeOperation) (store.ManagedJob, error) {
		return app.ComposeOperation(ctx, in.Name, in.Operation, in.RemoveVolumes)
	})
	register(b, "yard_compose_job", "Compose job status", "Read the persisted state of an accepted Compose job.", read, func(ctx context.Context, in targetInput) (store.ManagedJob, error) {
		job, found, err := app.ComposeJob(ctx, in.ID)
		if err == nil && !found {
			err = lifecycle.ErrNotFound
		}
		return job, err
	})
	register(b, "yard_projects_settings_get", "Projects settings", "Read the configured host projects base directory.", read, func(ctx context.Context, _ empty) (settingsInput, error) {
		base, err := app.ProjectsBase(ctx)
		return settingsInput{base}, err
	})
	register(b, "yard_projects_settings_set", "Set projects directory", "Validate and persist the projects base directory using existing Yard host checks.", setting, func(ctx context.Context, in settingsInput) (settingsInput, error) {
		base, err := app.SetProjectsBase(ctx, in.ProjectsBase)
		return settingsInput{base}, err
	})
	register(b, "yard_self_update_status", "Self-update status", "Read current self-update settings and state without triggering a check.", read, func(ctx context.Context, _ empty) (selfupdate.Status, error) { return app.UpdateStatus(ctx) })
	register(b, "yard_self_update_settings", "Configure self-updates", "Persist automatic-update settings. Enabling automatic updates can trigger a check that may install an update and restart Yard. This is not a read-only operation.", write, func(ctx context.Context, in updateSettings) (selfupdate.Status, error) {
		return app.SetUpdateSettings(ctx, in.Automatic, in.IntervalMinutes)
	})
	register(b, "yard_self_update_check_and_update", "Check and update NoX Yard", "Queue a manual update check. If a newer image is available, it may install it and restart NoX Yard even when automatic updates are disabled. Returns accepted/current status; observe completion using yard_self_update_status.", write, func(ctx context.Context, _ empty) (selfupdate.Status, error) { return app.CheckAndUpdate(ctx) })
	if b.err != nil {
		return nil, b.err
	}
	return noxmcp.New(noxmcp.Options{AppID: "nox-yard", Name: "nox-yard", Version: version, Timeout: Timeout, MaxInFlight: MaxInFlight, MaxPayloadBytes: MaxPayloadBytes, Tools: b.tools})
}
