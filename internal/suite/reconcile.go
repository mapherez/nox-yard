package suite

import (
	"context"
	"errors"
	"sort"
	"time"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/suite/backend"
)

// Job outcomes are committed by the existing workers. This consumes their durable
// outbox, never re-executes a Docker operation, and keeps registration separate.
func (c *Controller) reconcileDeployments(ctx context.Context) error {
	operations, err := c.data.SuiteOperations(ctx)
	if err != nil {
		return err
	}
	for _, op := range operations {
		bindings, err := c.data.SuiteBindings(ctx)
		if err != nil {
			return err
		}
		for _, id := range op.Bindings {
			for i := range bindings {
				b := &bindings[i]
				if b.ID != id || b.State == "removed" {
					continue
				}
				j, err := loadRecovery(*b)
				if err != nil {
					return err
				}
				if op.Status == "running" {
					j.Lifecycle = map[string]string{"start": "starting", "stop": "stopping", "restart": "restarting", "recreate": "recreating", "update": "updating", "sync": "updating"}[op.Operation]
				} else {
					j.Lifecycle = ""
				}
				if op.Status == "succeeded" && op.Operation == "remove" && op.Outcome != "recovery_required" && b.ProjectID == op.TargetID {
					b.State = "removing"
				}
				if err = c.save(b, j); err != nil {
					return err
				}
			}
		}
		if err = c.data.FinishSuiteOperation(op.Sequence); err != nil {
			return err
		}
	}
	bindings, err := c.data.SuiteBindings(ctx)
	if err != nil {
		return err
	}
	// Retire old associations before allowing a replacement service to claim the identity.
	sort.SliceStable(bindings, func(i, j int) bool { return bindings[i].State == "removing" && bindings[j].State != "removing" })
	for i := range bindings {
		b := &bindings[i]
		if b.State == "removed" {
			continue
		}
		j, err := loadRecovery(*b)
		if err != nil {
			return err
		}
		if j.Blocked || j.NextAttempt > time.Now().Unix() {
			continue
		}
		err = c.reconcileBinding(ctx, b, &j)
		if err == nil {
			j.Attempts = 0
			j.NextAttempt = 0
			b.Message = ""
		} else {
			j.Attempts++
			delay := time.Second * time.Duration(1<<min(j.Attempts, 6))
			j.NextAttempt = time.Now().Add(delay).Unix()
			var failure *backend.Failure
			j.Blocked = errors.As(err, &failure) && failure.Denied
			if j.Blocked {
				b.Message = "The paired installer is not authorized for this deployment. Update Backend if needed, then retry."
			} else {
				b.Message = "Suite integration is pending; the Docker deployment has not been repeated."
			}
		}
		if err = c.save(b, j); err != nil {
			return err
		}
	}
	return nil
}
func (c *Controller) reconcileBinding(ctx context.Context, b *store.SuiteBinding, j *recovery) error {
	if b.State == "removing" {
		return c.remove(ctx, b, j)
	}
	if c.inventory == nil {
		return errors.New("deployment inspection unavailable")
	}
	deployed, err := c.inventory.Inspect(ctx, b.ProjectID, b.Service, b.ProductID)
	if err != nil {
		return err
	}
	// A live replacement can temporarily remove a container or expose a
	// version that will still roll back. Await its existing job's final outcome
	// before retiring membership or accepting a changed declaration.
	active, err := c.data.Jobs(ctx, b.ProjectID, true)
	if err != nil {
		return err
	}
	if len(active) > 0 {
		if j.Record != nil && j.Lifecycle != "" {
			return c.report(ctx, b, j, j.Lifecycle)
		}
		return nil
	}
	if !deployed.Present {
		if j.Record != nil || j.GrantRevision != "" || j.PairingID != "" {
			b.State = "removing"
			return c.remove(ctx, b, j)
		}
		b.State = "removed"
		return nil
	}
	// A stopped participant has no listening socket. Its existing association
	// remains valid and the installer can still report its observed Docker state.
	if deployed.State != "running" {
		if j.Record != nil {
			return c.report(ctx, b, j, deployed.State)
		}
		return nil
	}
	declaration, err := c.declarationFor(ctx, b.ProductID)
	if err != nil {
		return err
	}
	value, _ := noxmcp.ParseSuiteJSON(declaration)
	fingerprint, err := noxmcp.Fingerprint(value)
	if err != nil {
		return err
	}
	if j.Fingerprint != "" && j.Fingerprint != fingerprint && j.PendingFingerprint != fingerprint {
		if err = c.resetPairing(ctx, b, j); err != nil {
			return err
		}
		j.GrantRevision = ""
		j.PendingFingerprint = fingerprint
		if err = c.save(b, *j); err != nil {
			return err
		}
	}
	if err = c.deployment(ctx, b, j); err != nil {
		b.State = "integration_pending"
		return err
	}
	if err = c.pair(ctx, b, j, declaration); err != nil {
		return err
	}
	if err = c.register(ctx, b, j, declaration); err != nil {
		return err
	}
	state := deployed.State
	if j.Lifecycle != "" {
		state = j.Lifecycle
	}
	j.ContainerIDs = deployed.ContainerIDs
	return c.report(ctx, b, j, state)
}
func (c *Controller) Retry(ctx context.Context, id string, revision int64) error {
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	bindings, err := c.data.SuiteBindings(ctx)
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if b.ID == id && b.Revision == revision && b.State != "removed" {
			j, err := loadRecovery(b)
			if err != nil {
				return err
			}
			j.Blocked = false
			j.Attempts = 0
			j.NextAttempt = 0
			j.DeliveryRetry = true
			b.Message = ""
			return c.save(&b, j)
		}
	}
	return store.ErrJobChanged
}
func (c *Controller) Snapshot(ctx context.Context) (any, error) {
	bindings, err := c.data.SuiteBindings(ctx)
	if err != nil {
		return nil, err
	}
	if bindings == nil {
		bindings = []store.SuiteBinding{}
	}
	return struct {
		Status      string               `json:"status"`
		Ready       bool                 `json:"ready"`
		Deployments []store.SuiteBinding `json:"deployments"`
	}{c.Status(), c.Ready(), bindings}, nil
}
