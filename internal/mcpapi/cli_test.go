package mcpapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

// Freeze the pre-menu tool contracts, excluding only presentation metadata.
func TestCLIMenusPreserveExistingToolContracts(t *testing.T) {
	raw, err := os.ReadFile("testdata/cli-tool-baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]string
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	tools, err := definitions(&application.Service{}, "baseline")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != len(want) {
		t.Fatal("tool catalog changed", len(tools), len(want))
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		raw, err := json.Marshal(struct {
			Name, Title, Description          string
			InputSchema, OutputSchema         any
			ReadOnly, Destructive, Idempotent bool
		}{tool.Name, tool.Title, tool.Description, tool.InputSchema, tool.OutputSchema, tool.ReadOnly, tool.Destructive, tool.Idempotent})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != want[tool.Name] {
			t.Fatal("existing tool contract changed", tool.Name)
		}
		path, _ := tool.Meta["cli"].(string)
		if path == "" || seen[path] {
			t.Fatal("missing/duplicate canonical CLI path", tool.Name)
		}
		seen[path] = true
	}
}

type cliInventory struct{}

func (cliInventory) Snapshot(context.Context) (inventory.Snapshot, error) {
	return inventory.Snapshot{CollectedAt: time.Now(), Projects: []inventory.Project{{ID: "compose:menu-test", Name: "Menu test", Kind: "compose", State: "running", Containers: []inventory.Container{{ID: strings.Repeat("a", 64), Name: "First"}, {ID: strings.Repeat("b", 64), Name: "Second"}}}}}, nil
}
func (cliInventory) InspectContainer(context.Context, string, bool) (inventory.ContainerInspection, error) {
	return inventory.ContainerInspection{}, nil
}

type cliFixture struct {
	server *httptest.Server
	token  func([]string) string
}

func newCLIFixture(t *testing.T) cliFixture {
	t.Helper()
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	app := application.New(data, inventory.NewNotifier())
	app.Inventory = cliInventory{}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kid := "menu-test"
	verification := noxmcp.SuiteVerificationOptions{Issuer: "urn:nox:backend:menu-test", Audience: "http://nox-yard/mcp/suite", ProductID: "nox-yard", AssociationID: "menu-association", GatewayServiceID: "menu-gateway", RegistrationRevision: "registration-1", ServiceCredentialRevision: "credential-1", Keys: map[string]noxmcp.SuiteSigningKey{kid: {Kty: "EC", Crv: "P-256", Kid: &kid, X: base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))), Y: base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32)))}}}
	token := func(scopes []string) string {
		now := time.Now().Unix()
		claims := map[string]any{"iss": verification.Issuer, "aud": verification.Audience, "sub": "menu-user", "act": map[string]string{"sub": verification.GatewayServiceID}, "iat": now, "nbf": now, "exp": now + 300, "jti": "menu-test", "scope": strings.Join(scopes, " "), "nox": map[string]string{"contractVersion": "1.0", "keyId": "menu-key", "authRevision": "auth-1", "productId": verification.ProductID, "registrationRevision": verification.RegistrationRevision, "associationId": verification.AssociationID, "serviceCredentialRevision": verification.ServiceCredentialRevision}}
		header, _ := json.Marshal(map[string]string{"alg": "ES256", "typ": "nox-delegation+jwt", "kid": kid})
		body, _ := json.Marshal(claims)
		input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
		digest := sha256.Sum256([]byte(input))
		r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return input + "." + base64.RawURLEncoding.EncodeToString(append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...))
	}
	handler, err := NewSuiteHandler(app, "menu-test", func(raw string) (noxmcp.SuiteVerificationOptions, *noxmcp.VerifiedSuiteIdentity, error) {
		id, err := noxmcp.VerifySuiteToken(raw, verification)
		return verification, id, err
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return cliFixture{server, token}
}
func (f cliFixture) rpc(t *testing.T, token, method string, params noxmcp.JSON) noxmcp.JSON {
	t.Helper()
	params["_meta"] = noxmcp.DiscoveryMeta()
	body, _ := json.Marshal(noxmcp.JSON{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(t.Context(), "POST", f.server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", noxmcp.DiscoveryProtocol)
	req.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		req.Header.Set("Nox-Request-Id", "12345678-1234-4234-8234-123456789abc")
		req.Header.Set("Nox-Deadline-At", time.Now().Add(30*time.Second).UTC().Format("2006-01-02T15:04:05.000Z"))
		req.Header.Set("Nox-Timeout-Ms", "30000")
	}
	if name, ok := params["name"].(string); ok {
		req.Header.Set("Mcp-Name", name)
	}
	res, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result noxmcp.JSON `json:"result"`
		Error  any         `json:"error"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil || res.StatusCode != 200 || envelope.Error != nil || envelope.Result == nil {
		t.Fatalf("%s failed: %d %s (%v)", method, res.StatusCode, raw, err)
	}
	return envelope.Result
}
func (f cliFixture) definitions(t *testing.T, token string) map[string]noxmcp.JSON {
	t.Helper()
	cache, err := noxmcp.NewDiscoveryCache(f.server.URL, "menu-test")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = cache.Sync(t.Context(), func(_ context.Context, method string, params noxmcp.JSON) (noxmcp.JSON, error) {
			return f.rpc(t, token, method, params), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return cache.Definitions()
}
func TestCLIDiscoveryKeepsMenusPrivateAndReferencesAuthorized(t *testing.T) {
	f := newCLIFixture(t)
	for _, scopes := range [][]string{{"nox:inspect"}, {"nox:inspect", "nox:write", "nox:sensitive-read"}} {
		token := f.token(scopes)
		defs := f.definitions(t, token)
		standard := f.rpc(t, token, "tools/list", noxmcp.JSON{})
		if len(defs) != len(standard["tools"].([]any)) {
			t.Fatal("catalogs disagree")
		}
		if len(scopes) == 3 && len(defs) != 38 {
			t.Fatal("missing administrator tools", len(defs))
		}
		for _, entry := range standard["tools"].([]any) {
			tool := entry.(map[string]any)
			name := tool["name"].(string)
			d := defs[name]
			cli, ok := d["cli"].(map[string]any)
			if !ok {
				t.Fatal("missing CLI descriptor", name)
			}
			if _, err := noxmcp.InterpretCLI(cli); err != nil {
				t.Fatal(name, err)
			}
			var canonical noxmcp.JSON
			b, _ := json.Marshal(d["tool"])
			json.Unmarshal(b, &canonical)
			meta := canonical["_meta"].(map[string]any)
			if meta["cli"] != cliPaths[name] || meta["nox/authorization"] == nil {
				t.Fatal("canonical metadata lost", name)
			}
			delete(meta, "cli")
			if !reflect.DeepEqual(canonical, tool) {
				t.Fatal("standard schemas/policy changed or menu data leaked", name)
			}
			var references func(any)
			references = func(v any) {
				switch v := v.(type) {
				case map[string]any:
					if ref, ok := v["tool"].(string); ok && defs[ref] == nil {
						t.Fatal("menu references unauthorized tool", name, ref)
					}
					for _, child := range v {
						references(child)
					}
				case []any:
					for _, child := range v {
						references(child)
					}
				}
			}
			references(cli)
		}
		if len(scopes) == 1 && defs["yard_container_environment"] != nil {
			t.Fatal("sensitive tools leaked")
		}
		if len(scopes) == 3 {
			exportCLICatalog(t, defs)
		}
	}
}
func TestCLIMenuSelectorsUseExistingProjectIDs(t *testing.T) {
	f := newCLIFixture(t)
	token := f.token([]string{"nox:inspect"})
	defs := f.definitions(t, token)
	d := defs["yard_project_remove_preview"]
	cli := d["cli"].(map[string]any)
	source := cli["parameters"].(map[string]any)["/id"].(map[string]any)["options"].(map[string]any)
	if source["tool"] != "yard_projects" || source["itemsPointer"] != "/projects" || source["valuePointer"] != "/id" {
		t.Fatal(source)
	}
	result := f.rpc(t, token, "tools/call", noxmcp.JSON{"name": source["tool"], "arguments": noxmcp.JSON{}})
	if result["isError"] == true {
		t.Fatal(result)
	}
	projects := result["structuredContent"].(map[string]any)["projects"].([]any)
	if projects[0].(map[string]any)["id"] != "compose:menu-test" {
		t.Fatal(projects)
	}
}
func exportCLICatalog(t *testing.T, defs map[string]noxmcp.JSON) {
	t.Helper()
	file := os.Getenv("NOX_YARD_TEST_CLI_EXPORT")
	if file == "" {
		return
	}
	raw, _ := json.Marshal(noxmcp.JSON{"contractVersion": "1.0", "productId": "nox-yard", "revision": "registration-1", "associationId": "menu-association", "declaration": noxmcp.JSON{"contractVersion": "1.0", "productId": "nox-yard", "productVersion": "test", "displayName": "NoX Yard", "capabilities": noxmcp.JSON{"suiteMcp": noxmcp.JSON{"path": "/mcp/suite"}}}, "connection": noxmcp.JSON{"baseUrl": "http://nox-yard"}})
	record, err := noxmcp.LoadSuiteRegistrationRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	source := noxmcp.SuiteGatewaySource{ContractVersion: "1.0", Registration: record, ServerID: "nox-yard", ViewID: "menu-view"}
	for _, d := range defs {
		raw, _ := json.Marshal(d)
		var definition noxmcp.SuiteSourceDefinition
		if err = json.Unmarshal(raw, &definition); err != nil {
			t.Fatal(err)
		}
		source.Definitions = append(source.Definitions, definition)
	}
	snapshot, err := noxmcp.ComposeSuiteGateway(noxmcp.SuiteGatewayOptions{ServerID: "menu-gateway", AuthorizationViewID: strings.Repeat("a", 43), RegistryRevision: "registry-1", PolicyRevision: "1.0", Sources: []noxmcp.SuiteGatewaySource{source}, NowMS: time.Now().UnixMilli(), AuthorizationExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
