package suite

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/suite/backend"
	"github.com/mapherez/nox-yard/internal/suite/local"
)

type recovery struct {
	InstallerID        string                          `json:"installerId"`
	Keys               map[string]string               `json:"keys"`
	TrustRevision      string                          `json:"trustRevision"`
	PendingFingerprint string                          `json:"pendingFingerprint"`
	GrantRevision      string                          `json:"grantRevision"`
	Intent             *noxmcp.SuitePairingIntent      `json:"intent,omitempty"`
	PairingID          string                          `json:"pairingId"`
	DeliveryStarted    bool                            `json:"deliveryStarted"`
	DeliveryRetry      bool                            `json:"deliveryRetry"`
	Record             *noxmcp.SuiteRegistrationRecord `json:"record,omitempty"`
	Fingerprint        string                          `json:"fingerprint"`
	Epoch              string                          `json:"epoch"`
	Sequence           string                          `json:"sequence"`
	EpochKey           string                          `json:"epochKey"`
	Lifecycle          string                          `json:"lifecycle"`
	ContainerIDs       []string                        `json:"containerIds"`
	NextAttempt        int64                           `json:"nextAttempt"`
	Attempts           int                             `json:"attempts"`
	Blocked            bool                            `json:"blocked"`
}

func loadRecovery(b store.SuiteBinding) (recovery, error) {
	var j recovery
	err := json.Unmarshal(b.Private, &j)
	if j.Keys == nil {
		j.Keys = map[string]string{}
	}
	if j.Sequence == "" {
		j.Sequence = "0"
	}
	return j, err
}
func (c *Controller) save(b *store.SuiteBinding, j recovery) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	b.Private = raw
	if err = c.data.SaveSuiteBinding(*b); err != nil {
		return err
	}
	b.Revision++
	return nil
}
func (c *Controller) key(b *store.SuiteBinding, j *recovery, name string) (string, error) {
	if key := j.Keys[name]; key != "" {
		return key, nil
	}
	j.Keys[name] = rand.Text()
	return j.Keys[name], c.save(b, *j)
}
func (c *Controller) request(ctx context.Context, method, path, key, revision string, body any) ([]byte, int, error) {
	return c.trust.ConditionalRequest(ctx, method, path, key, revision, body)
}
func owned(r noxmcp.SuiteRegistrationRecord, b store.SuiteBinding) bool {
	return r.ProductID == b.ProductID && r.ManagedBy != nil && r.ManagedBy.ProductID == "nox-yard" && r.ManagedBy.DeploymentID == b.ID && r.Connection.BaseUrl == local.Origin(b.ProductID)
}
func (c *Controller) readRecord(ctx context.Context, product string) (noxmcp.SuiteRegistrationRecord, int, error) {
	raw, status, err := c.request(ctx, "GET", "/api/nox/products/"+backend.Resource(product), "", "", nil)
	if err != nil {
		return noxmcp.SuiteRegistrationRecord{}, status, err
	}
	r, err := noxmcp.LoadSuiteRegistrationRecord(raw)
	return r, status, err
}
func (c *Controller) deployment(ctx context.Context, b *store.SuiteBinding, j *recovery) error {
	p, _, err := c.trust.Authority()
	if err != nil {
		return err
	}
	if j.InstallerID != p.Service.ServiceID {
		j.InstallerID = p.Service.ServiceID
		j.GrantRevision = ""
		j.TrustRevision = ""
		j.Keys["grant"] = ""
		j.Epoch = ""
		j.EpochKey = ""
		j.Sequence = "0"
		if err = c.save(b, *j); err != nil {
			return err
		}
	}
	if j.GrantRevision != "" {
		return nil
	}
	raw, _, err := c.request(ctx, "GET", "/api/nox/services/"+backend.Resource(j.InstallerID)+"/deployments", "", "", nil)
	if err != nil {
		return err
	}
	var grants []struct {
		DeploymentID  string `json:"deploymentId"`
		ProductID     string `json:"productId"`
		BaseURL       string `json:"baseUrl"`
		Revision      string `json:"revision"`
		TrustRevision string `json:"trustRevision"`
	}
	if err = json.Unmarshal(raw, &grants); err != nil {
		return err
	}
	for _, grant := range grants {
		if grant.DeploymentID == b.ID && grant.ProductID == b.ProductID && grant.BaseURL == local.Origin(b.ProductID) && grant.Revision != "" {
			j.GrantRevision = grant.Revision
			j.TrustRevision = grant.TrustRevision
			return c.save(b, *j)
		}
	}
	key, err := c.key(b, j, "grant")
	if err != nil {
		return err
	}
	raw, _, err = c.request(ctx, "PUT", "/api/nox/services/"+backend.Resource(j.InstallerID)+"/deployments/"+backend.Resource(b.ID), key, "*", map[string]string{"productId": b.ProductID, "baseUrl": local.Origin(b.ProductID)})
	if err != nil {
		return err
	}
	var grant struct {
		Revision      string `json:"revision"`
		ProductID     string `json:"productId"`
		DeploymentID  string `json:"deploymentId"`
		TrustRevision string `json:"trustRevision"`
	}
	if json.Unmarshal(raw, &grant) != nil || grant.Revision == "" || grant.ProductID != b.ProductID || grant.DeploymentID != b.ID {
		return errors.New("invalid deployment association")
	}
	j.GrantRevision = grant.Revision
	j.TrustRevision = grant.TrustRevision
	return c.save(b, *j)
}
func (c *Controller) declarationFor(ctx context.Context, product string) (json.RawMessage, error) {
	t, err := local.NewLocal(c.cfg.Directory, local.Origin(product))
	if err != nil {
		return nil, err
	}
	defer t.Close()
	req, err := http.NewRequestWithContext(ctx, "GET", local.Origin(product)+"/nox/integration", nil)
	if err != nil {
		return nil, err
	}
	res, err := t.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("product declaration is unavailable")
	}
	raw, err := readBody(res)
	if err != nil {
		return nil, err
	}
	declaration, err := noxmcp.LoadSuiteProductDeclaration(raw)
	if err != nil {
		return raw, err
	}
	if declaration.ProductID != product {
		return raw, errors.New("product declaration identity mismatch")
	}
	return raw, nil
}
func (c *Controller) deliver(ctx context.Context, b store.SuiteBinding, intent noxmcp.SuitePairingIntent, key string) error {
	t, err := local.NewLocal(c.cfg.Directory, local.Origin(b.ProductID))
	if err != nil {
		return err
	}
	defer t.Close()
	raw, err := json.Marshal(map[string]any{"contractVersion": "1.0", "backendOrigin": "http://nox-backend", "intent": intent})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", local.Origin(b.ProductID)+"/nox/integration/pairing", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	req.GetBody = nil
	res, err := t.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 202 && res.StatusCode != 204 {
		return errors.New("participant pairing adapter refused delivery")
	}
	return nil
}
func (c *Controller) pair(ctx context.Context, b *store.SuiteBinding, j *recovery, declaration json.RawMessage) error {
	// An acknowledged association from a lost registration response can be reused.
	if current, _, err := c.readRecord(ctx, b.ProductID); err == nil {
		if !owned(current, *b) {
			return &backend.Failure{Code: "REGISTRATION_CONFLICT"}
		}
		previous := j.Record
		j.Record = &current
		existing, _ := noxmcp.ParseSuiteJSON(mustJSON(current.Declaration))
		actual, _ := noxmcp.ParseSuiteJSON(declaration)
		existingFP, _ := noxmcp.Fingerprint(existing)
		actualFP, _ := noxmcp.Fingerprint(actual)
		if existingFP == actualFP {
			// A registration response may have been lost after Backend committed.
			// Adopt that accepted result instead of reusing the old mutation key
			// with a changed If-Match precondition.
			if previous == nil || previous.Revision != current.Revision {
				j.Epoch = ""
				j.EpochKey = ""
				j.Sequence = "0"
			}
			j.Intent = nil
			j.Fingerprint = actualFP
			j.PendingFingerprint = ""
			b.State = "registered"
			return c.save(b, *j)
		}
	}
	if j.PairingID == "" {
		key, err := c.key(b, j, "pairing")
		if err != nil {
			return err
		}
		raw, _, err := c.request(ctx, "POST", "/api/nox/pairings", key, pairingRevision(j.TrustRevision), map[string]any{"contractVersion": "1.0", "productId": b.ProductID, "purpose": "product", "baseUrl": local.Origin(b.ProductID), "managedBy": map[string]string{"productId": "nox-yard", "deploymentId": b.ID}})
		if err != nil {
			return err
		}
		intent, err := noxmcp.LoadSuitePairingIntent(raw)
		if err != nil {
			return err
		}
		if intent.ProductID != b.ProductID || intent.BaseUrl != local.Origin(b.ProductID) || intent.ManagedBy == nil || intent.ManagedBy.DeploymentID != b.ID {
			return errors.New("pairing response binding mismatch")
		}
		j.Intent = &intent
		j.PairingID = intent.PairingID
		if err = c.save(b, *j); err != nil {
			return err
		}
	}
	raw, _, err := c.request(ctx, "GET", "/api/nox/pairings/"+backend.Resource(j.PairingID), "", "", nil)
	if err != nil {
		return err
	}
	var state struct {
		State, Revision string
		ExpiresAt       string `json:"expiresAt"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return errors.New("invalid pairing state")
	}
	if state.State == "acknowledged" {
		j.Intent = nil
		return c.save(b, *j)
	}
	expiry, err := noxmcp.ParseSuiteTimestamp(state.ExpiresAt)
	if err != nil {
		return err
	}
	if !time.Now().Before(expiry) || state.State == "expired" || state.State == "cancelled" {
		if !j.DeliveryRetry {
			return &backend.Failure{Code: "PAIRING_EXPIRED"}
		}
		key, err := c.key(b, j, "cancel-pairing")
		if err != nil {
			return err
		}
		if state.State != "cancelled" {
			if _, _, err = c.request(ctx, "DELETE", "/api/nox/pairings/"+backend.Resource(j.PairingID), key, state.Revision, nil); err != nil {
				return err
			}
		}
		j.PairingID = ""
		j.Intent = nil
		j.DeliveryStarted = false
		j.DeliveryRetry = false
		j.Keys["pairing"] = ""
		j.Keys["cancel-pairing"] = ""
		j.Keys["delivery"] = ""
		return c.save(b, *j)
	}
	if j.DeliveryStarted && !j.DeliveryRetry {
		return &backend.Failure{Code: "PAIRING_PENDING", Retryable: true}
	}
	if j.Intent == nil {
		return &backend.Failure{Code: "PAIRING_RECOVERY_REQUIRED"}
	}
	key, err := c.key(b, j, "delivery")
	if err != nil {
		return err
	}
	j.DeliveryStarted = true
	j.DeliveryRetry = false
	if err = c.save(b, *j); err != nil {
		return err
	}
	deliveryErr := c.deliver(ctx, *b, *j.Intent, key)
	// A receipt or timeout is not acknowledgement. Re-read before deciding to retry.
	raw, _, err = c.request(ctx, "GET", "/api/nox/pairings/"+backend.Resource(j.PairingID), "", "", nil)
	if err == nil && json.Unmarshal(raw, &state) == nil && state.State == "acknowledged" {
		j.Intent = nil
		return c.save(b, *j)
	}
	if deliveryErr != nil {
		return &backend.Failure{Code: "PAIRING_DELIVERY_UNCONFIRMED"}
	}
	return &backend.Failure{Code: "PAIRING_PENDING", Retryable: true}
}
func (c *Controller) register(ctx context.Context, b *store.SuiteBinding, j *recovery, raw json.RawMessage) error {
	value, err := noxmcp.ParseSuiteJSON(raw)
	if err != nil {
		return err
	}
	fingerprint, err := noxmcp.Fingerprint(value)
	if err != nil {
		return err
	}
	if j.Record != nil && j.Fingerprint == fingerprint {
		return nil
	}
	key, err := c.key(b, j, "register:"+fingerprint)
	if err != nil {
		return err
	}
	revision := "*"
	if j.Record != nil {
		revision = j.Record.Revision
	}
	encoded, _, err := c.request(ctx, "PUT", "/api/nox/products/"+backend.Resource(b.ProductID), key, revision, noxmcp.SuiteRegistrationRequest{ContractVersion: "1.0", BaseUrl: local.Origin(b.ProductID), ManagedBy: &noxmcp.SuiteRegistrationRecordManagedBy{ProductID: "nox-yard", DeploymentID: b.ID}})
	if err != nil {
		return err
	}
	record, err := noxmcp.LoadSuiteRegistrationRecord(encoded)
	if err != nil || !owned(record, *b) {
		return errors.New("registration response binding mismatch")
	}
	if j.Record == nil || j.Record.Revision != record.Revision {
		j.Epoch = ""
		j.EpochKey = ""
		j.Sequence = "0"
	}
	j.Record = &record
	j.Fingerprint = fingerprint
	j.PendingFingerprint = ""
	b.State = "registered"
	b.Message = ""
	return c.save(b, *j)
}
func (c *Controller) report(ctx context.Context, b *store.SuiteBinding, j *recovery, state string) error {
	if j.Record == nil {
		return nil
	}
	p, _, err := c.trust.Authority()
	if err != nil {
		return err
	}
	path := "/api/nox/products/" + backend.Resource(b.ProductID)
	if j.Epoch == "" {
		if j.EpochKey == "" {
			j.EpochKey = rand.Text()
			if err = c.save(b, *j); err != nil {
				return err
			}
		}
		raw, _, err := c.request(ctx, "POST", path+"/observation-epochs", j.EpochKey, "", noxmcp.SuiteObservationEpochRequest{ContractVersion: "1.0", ProductID: b.ProductID, AssociationID: j.Record.AssociationID, RegistrationRevision: j.Record.Revision, ProducerID: p.Service.ServiceID, Source: "yard"})
		if err != nil {
			return err
		}
		epoch, err := noxmcp.LoadSuiteObservationEpoch(raw)
		if err != nil || epoch.ProducerID != p.Service.ServiceID || epoch.ProductID != b.ProductID || epoch.AssociationID != j.Record.AssociationID {
			return errors.New("invalid observation epoch")
		}
		j.Epoch = epoch.ProducerEpoch
	}
	n, ok := new(big.Int).SetString(j.Sequence, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 64 {
		return errors.New("invalid observation sequence")
	}
	n.Add(n, big.NewInt(1))
	if n.BitLen() > 64 {
		return errors.New("observation epoch exhausted")
	}
	j.Sequence = n.String()
	if err = c.save(b, *j); err != nil {
		return err
	}
	_, _, err = c.request(ctx, "POST", path+"/observations", rand.Text(), "", map[string]any{"contractVersion": "1.0", "productId": b.ProductID, "associationId": j.Record.AssociationID, "registrationRevision": j.Record.Revision, "source": "yard", "producerId": p.Service.ServiceID, "producerEpoch": j.Epoch, "sequence": j.Sequence, "observedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "state": state, "managedBy": map[string]string{"productId": "nox-yard", "deploymentId": b.ID}})
	return err
}
func (c *Controller) remove(ctx context.Context, b *store.SuiteBinding, j *recovery) error {
	if j.Record != nil {
		key, err := c.key(b, j, "remove-registration")
		if err != nil {
			return err
		}
		_, status, err := c.request(ctx, "DELETE", "/api/nox/products/"+backend.Resource(b.ProductID), key, j.Record.Revision, nil)
		if err != nil && status != 404 {
			current, s, readErr := c.readRecord(ctx, b.ProductID)
			if readErr == nil && owned(current, *b) && current.AssociationID == j.Record.AssociationID && current.Revision != j.Record.Revision {
				j.Record = &current
				j.Keys["remove-registration"] = ""
				return c.save(b, *j)
			}
			if s != 404 && (readErr != nil || owned(current, *b)) {
				return err
			}
			// A replacement association is never deleted by an old deployment's cleanup.
		}
		j.Record = nil
		if err = c.save(b, *j); err != nil {
			return err
		}
	}
	if j.PairingID != "" && j.Intent != nil {
		raw, _, err := c.request(ctx, "GET", "/api/nox/pairings/"+backend.Resource(j.PairingID), "", "", nil)
		if err != nil {
			return err
		}
		var p struct{ State, Revision string }
		if json.Unmarshal(raw, &p) != nil {
			return errors.New("invalid pairing state")
		}
		if p.State != "acknowledged" && p.State != "cancelled" {
			key, err := c.key(b, j, "cancel-pairing")
			if err != nil {
				return err
			}
			if _, _, err = c.request(ctx, "DELETE", "/api/nox/pairings/"+backend.Resource(j.PairingID), key, p.Revision, nil); err != nil {
				return err
			}
		}
		j.Intent = nil
		if err = c.save(b, *j); err != nil {
			return err
		}
	}
	if j.GrantRevision != "" {
		key, err := c.key(b, j, "remove-grant")
		if err != nil {
			return err
		}
		_, status, err := c.request(ctx, "DELETE", "/api/nox/services/"+backend.Resource(j.InstallerID)+"/deployments/"+backend.Resource(b.ID), key, j.GrantRevision, nil)
		if err != nil && status != 404 {
			if status != 412 {
				return err
			}
			raw, _, readErr := c.request(ctx, "GET", "/api/nox/services/"+backend.Resource(j.InstallerID)+"/deployments", "", "", nil)
			if readErr != nil {
				return readErr
			}
			var grants []struct{ DeploymentID, ProductID, BaseURL, Revision string }
			if json.Unmarshal(raw, &grants) != nil {
				return errors.New("invalid deployment list")
			}
			for _, grant := range grants {
				if grant.DeploymentID != b.ID {
					continue
				}
				if grant.ProductID != b.ProductID || grant.BaseURL != local.Origin(b.ProductID) {
					return errors.New("deployment removal ownership changed")
				}
				j.GrantRevision = grant.Revision
				j.Keys["remove-grant"] = ""
				return c.save(b, *j)
			}
			// The original delete committed but its response/replay window was
			// lost. Absence from this installer's own list confirms retirement.
		}
		j.GrantRevision = ""
		if err = c.save(b, *j); err != nil {
			return err
		}
	}

	b.State = "removed"
	b.Message = ""
	return c.save(b, recovery{Keys: map[string]string{}})
}

func pairingRevision(revision string) string {
	if revision == "" {
		return "*"
	}
	return revision
}
func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }

func (c *Controller) resetPairing(ctx context.Context, b *store.SuiteBinding, j *recovery) error {
	if j.PairingID != "" && j.Intent != nil {
		raw, _, err := c.request(ctx, "GET", "/api/nox/pairings/"+backend.Resource(j.PairingID), "", "", nil)
		if err != nil {
			return err
		}
		var state struct{ State, Revision string }
		if json.Unmarshal(raw, &state) != nil {
			return errors.New("invalid pairing state")
		}
		if state.State != "acknowledged" && state.State != "cancelled" {
			key, err := c.key(b, j, "cancel:"+j.PairingID)
			if err != nil {
				return err
			}
			if _, _, err = c.request(ctx, "DELETE", "/api/nox/pairings/"+backend.Resource(j.PairingID), key, state.Revision, nil); err != nil {
				return err
			}
		}
	}
	j.PairingID = ""
	j.Intent = nil
	j.DeliveryStarted = false
	j.DeliveryRetry = true
	for _, name := range []string{"pairing", "delivery", "cancel-pairing"} {
		j.Keys[name] = ""
	}
	return nil
}
