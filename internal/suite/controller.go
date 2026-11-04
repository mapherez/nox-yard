// Package suite adapts Yard's existing local deployment service to NoX Suite.
package suite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/suite/backend"
	"github.com/mapherez/nox-yard/internal/suite/trust"
)

const MCPPath = "/mcp/suite"
const ProductLabel = "io.nox.suite.product-id"

type Config struct {
	Directory, VolumeName string
	ReportInterval        time.Duration
}
type privateTrust struct{ Data *store.Store }

func (s privateTrust) LoadTrust() ([]byte, error) { return s.Data.SuitePrivate("trust") }
func (s privateTrust) SaveTrust(b []byte) error   { return s.Data.SaveSuitePrivate("trust", b) }

type barrier struct {
	UserID, KeyID, ServiceID, CredentialRevision, ProductID, AssociationID string
	All                                                                    bool
	Before                                                                 int64
}
type registryState struct {
	Snapshot noxmcp.SuiteRegistrySnapshot `json:"snapshot"`
	Barriers []barrier                    `json:"barriers"`
}
type Controller struct {
	cfg         Config
	data        *store.Store
	trust       *trust.Engine
	declaration json.RawMessage
	mu          sync.RWMutex
	reconcileMu sync.Mutex
	registry    registryState
	ready       bool
	inventory   Runtime
}

func Declaration(version string) (json.RawMessage, error) {
	if version == "" {
		version = "dev"
	}
	b, err := json.Marshal(noxmcp.SuiteProductDeclaration{ContractVersion: "1.0", ProductID: "nox-yard", ProductVersion: version, DisplayName: "NoX Yard", Capabilities: noxmcp.SuiteProductDeclarationCapabilities{SuiteMcp: &noxmcp.SuiteProductDeclarationCapabilitiesSuiteMcp{Path: MCPPath}}})
	if err == nil {
		_, err = noxmcp.LoadSuiteProductDeclaration(b)
	}
	return b, err
}
func New(c Config, data *store.Store, version string, runtime Runtime) (*Controller, error) {
	if c.Directory != "" && (!filepath.IsAbs(c.Directory) || strings.ContainsAny(c.Directory, "\x00\r\n")) {
		return nil, errors.New("NOX_YARD_SUITE_SOCKET_DIR must be an absolute local directory")
	}
	if c.ReportInterval == 0 {
		c.ReportInterval = 15 * time.Second
	}
	if c.ReportInterval < time.Second || c.ReportInterval > time.Hour {
		return nil, errors.New("invalid Suite reporting interval")
	}
	if c.VolumeName == "" {
		c.VolumeName = "nox-suite-local"
	}
	decl, err := Declaration(version)
	if err != nil {
		return nil, err
	}
	cfg := trust.Config{SocketDir: c.Directory}
	if c.Directory != "" {
		cfg.BackendURL = "http://nox-backend"
	}
	engine, err := trust.New(cfg, privateTrust{data}, decl, slog.Default())
	if err != nil {
		return nil, err
	}
	out := &Controller{cfg: c, data: data, trust: engine, declaration: decl, inventory: runtime}
	raw, err := data.SuitePrivate("registry")
	if err != nil {
		engine.Close()
		return nil, err
	}
	if raw != nil {
		if _, err = noxmcp.ParseSuiteJSONWithLimit(raw, 8<<20); err != nil {
			engine.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &out.registry); err != nil {
			engine.Close()
			return nil, err
		}
	}
	return out, nil
}
func (c *Controller) Close() { c.trust.Close() }
func (c *Controller) Declaration() json.RawMessage {
	return append(json.RawMessage{}, c.declaration...)
}
func (c *Controller) Status() string { return c.trust.Status() }
func (c *Controller) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, _, err := c.trust.Authority()
	return c.ready && err == nil
}
func (c *Controller) LocalHandler(mcp http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"code": code})
		}
		if r.Host != "nox-yard" {
			fail(400, "INVALID_DESTINATION")
			return
		}
		switch r.URL.Path {
		case "/nox/integration":
			if r.Method != "GET" {
				fail(405, "METHOD_NOT_ALLOWED")
				return
			}
			w.Write(c.declaration)
		case "/nox/integration/pairing":
			if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Idempotency-Key") == "" {
				fail(400, "INVALID_PAIRING")
				return
			}
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(20 * time.Second))
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, noxmcp.SuiteMaxPayloadBytes))
			r.Body.Close()
			var in struct {
				ContractVersion string                    `json:"contractVersion"`
				BackendOrigin   string                    `json:"backendOrigin"`
				Intent          noxmcp.SuitePairingIntent `json:"intent"`
			}
			if err != nil {
				fail(413, "PAYLOAD_LIMIT")
				return
			}
			if _, err = noxmcp.ParseSuiteJSON(raw); err != nil || json.Unmarshal(raw, &in) != nil || in.ContractVersion != "1.0" || in.BackendOrigin != "http://nox-backend" {
				fail(400, "INVALID_PAIRING")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if err = c.trust.Receive(ctx, in.Intent); err != nil {
				fail(503, "PAIRING_RECONCILIATION_REQUIRED")
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"accepted": true})
		case MCPPath:
			mcp.ServeHTTP(w, r)
		default:
			fail(404, "NOT_FOUND")
		}
	})
}

// Unverified fields below select a profile only. Identity is returned solely
// after ES256 verification against the live configured issuer and local product.
func (c *Controller) Admit(token string) (noxmcp.SuiteVerificationOptions, *noxmcp.VerifiedSuiteIdentity, error) {
	var opts noxmcp.SuiteVerificationOptions
	p, keys, err := c.trust.Authority()
	if err != nil {
		return opts, nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	var own noxmcp.SuiteRegistrationRecord
	for _, r := range c.registry.Snapshot.Products {
		if r.ProductID == "nox-yard" {
			own = r
		}
	}
	if !c.ready || own.AssociationID != p.Service.AssociationID || own.Declaration.Capabilities.SuiteMcp == nil || own.Declaration.Capabilities.SuiteMcp.Path != MCPPath {
		return opts, nil, errors.New("Suite is not ready")
	}
	if len(token) > noxmcp.SuiteMaxTokenBytes {
		return opts, nil, errors.New("invalid token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return opts, nil, errors.New("invalid token")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return opts, nil, err
	}
	value, err := noxmcp.ParseSuiteJSON(raw)
	if err != nil {
		return opts, nil, err
	}
	claims, ok := value.(map[string]any)
	if !ok {
		return opts, nil, errors.New("invalid token")
	}
	actor, _ := claims["sub"].(string)
	if act, ok := claims["act"].(map[string]any); ok {
		actor, _ = act["sub"].(string)
	}
	nox, _ := claims["nox"].(map[string]any)
	revision, _ := nox["serviceCredentialRevision"].(string)
	opts = noxmcp.SuiteVerificationOptions{Issuer: p.Issuer, Audience: "http://nox-yard" + MCPPath, ProductID: "nox-yard", AssociationID: own.AssociationID, RegistrationRevision: own.Revision, GatewayServiceID: actor, ServiceCredentialRevision: revision, Keys: map[string]noxmcp.SuiteSigningKey{}, Now: time.Now()}
	for _, k := range keys.Keys {
		if k.Kid != nil {
			opts.Keys[*k.Kid] = k
		}
	}
	id, err := noxmcp.VerifySuiteToken(token, opts)
	if err != nil {
		return opts, nil, err
	}
	for _, b := range c.registry.Barriers {
		if matches(b, id) {
			return opts, nil, errors.New("authorization invalidated")
		}
	}
	return opts, id, nil
}
func matches(b barrier, id *noxmcp.VerifiedSuiteIdentity) bool {
	claims, ctx := id.Claims(), id.Context()
	iat, _ := claims["iat"].(float64)
	if int64(iat) >= b.Before {
		return false
	}
	actor, _ := ctx["gatewayServiceId"].(string)
	if id.Kind() == "probe" {
		actor, _ = ctx["serviceId"].(string)
	}
	pairs := [][2]string{{b.UserID, text(ctx["userId"])}, {b.KeyID, text(ctx["keyId"])}, {b.ServiceID, actor}, {b.CredentialRevision, text(ctx["serviceCredentialRevision"])}, {b.ProductID, text(ctx["productId"])}, {b.AssociationID, text(ctx["associationId"])}}
	for _, p := range pairs {
		if p[0] != "" && p[0] != p[1] {
			return false
		}
	}
	return b.All || b.UserID != "" || b.KeyID != "" || b.ServiceID != "" || b.ProductID != "" || b.AssociationID != ""
}
func text(v any) string { s, _ := v.(string); return s }
func (c *Controller) fullRegistry(ctx context.Context) error {
	raw, _, err := c.trust.Request(ctx, "GET", "/api/nox/products", "", nil)
	if err != nil {
		return err
	}
	snapshot, err := noxmcp.LoadSuiteRegistrySnapshot(raw)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	next := c.registry
	next.Snapshot = snapshot
	// A truncated feed cannot preserve possibly missed revocations.
	next.Barriers = append(next.Barriers, barrier{All: true, Before: time.Now().Unix() + 1})
	return c.commitRegistry(next)
}
func (c *Controller) commitRegistry(next registryState) error {
	now := time.Now().Unix()
	kept := []barrier{}
	for _, b := range next.Barriers {
		if b.Before+305 > now {
			kept = append(kept, b)
		}
	}
	if len(kept) > 8192 {
		kept = []barrier{{All: true, Before: now + 1}}
	}
	next.Barriers = kept
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = c.data.SaveSuitePrivate("registry", raw); err != nil {
		return err
	}
	c.registry = next
	c.ready = true
	return nil
}
func (c *Controller) poll(ctx context.Context, wait int) error {
	c.mu.RLock()
	current := c.registry.Snapshot
	c.mu.RUnlock()
	if current.Cursor == "" {
		return c.fullRegistry(ctx)
	}
	raw, _, err := c.trust.Request(ctx, "GET", "/api/nox/events?after="+url.QueryEscape(current.Cursor)+"&waitMs="+textNumber(wait)+"&limit=256", "", nil)
	if err != nil {
		var f *backend.Failure
		if errors.As(err, &f) && f.Code == "CURSOR_EXPIRED" {
			return c.fullRegistry(ctx)
		}
		return err
	}
	snapshot, err := noxmcp.ReduceSuiteRegistry(current, raw, current.Cursor)
	if err != nil {
		return err
	}
	page, err := noxmcp.LoadSuiteEventPage(raw)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	next := c.registry
	next.Snapshot = snapshot
	for _, e := range page.Events {
		if e.Type != "authorization.invalidated" && e.Type != "service.invalidated" {
			continue
		}
		data := map[string]any{}
		encoded, _ := json.Marshal(e.Data)
		json.Unmarshal(encoded, &data)
		occurred, _ := noxmcp.ParseSuiteTimestamp(e.OccurredAt)
		next.Barriers = append(next.Barriers, barrier{All: data["all"] == true, UserID: text(data["userId"]), KeyID: text(data["keyId"]), ServiceID: text(data["serviceId"]), CredentialRevision: text(data["credentialRevision"]), ProductID: text(data["productId"]), AssociationID: text(data["associationId"]), Before: occurred.Unix() + 1})
	}
	return c.commitRegistry(next)
}
