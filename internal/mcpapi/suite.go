package mcpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/machineauth"
)

// SuiteAdmission is supplied by the local participant, never browser headers or
// the independent Yard machine token/settings. The original tool bodies are reused.
type SuiteAdmission func(string) (noxmcp.SuiteVerificationOptions, *noxmcp.VerifiedSuiteIdentity, error)

func NewSuiteHandler(app *application.Service, version string, admit SuiteAdmission) (http.Handler, error) {
	tools, err := definitions(app, version)
	if err != nil {
		return nil, err
	}
	policies := map[string]noxmcp.SuitePolicy{}
	for i := range tools {
		tool := &tools[i]
		scopes := []string{"nox:inspect"}
		if !tool.ReadOnly {
			scopes = append(scopes, "nox:write")
		}
		switch tool.Name {
		case "yard_container_environment", "yard_container_logs", "yard_container_log_history", "yard_project_override_preview", "yard_project_override_apply":
			scopes = append(scopes, "nox:sensitive-read")
		}
		policies[tool.Name] = noxmcp.SuitePolicy{Version: "1.0", RequiredScopes: scopes, ArgumentDependent: tool.Name == "yard_project_configuration_read"}
		execute := tool.Execute
		tool.Execute = func(exec noxmcp.ExecutionContext, args map[string]any) (map[string]any, error) {
			if err := exec.CheckActive(); err != nil {
				return nil, err
			}
			exec.Context = machineauth.WithPolicy(exec.Context, machineauth.Policy{Write: slices.Contains(exec.Scopes, "nox:write"), Secrets: slices.Contains(exec.Scopes, "nox:sensitive-read")})
			return execute(exec, args)
		}
	}
	slots := make(chan struct{}, MaxInFlight)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		failure := func(status int, code string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"code": code})
		}
		if r.Method != "POST" || r.URL.RawQuery != "" {
			failure(400, "INVALID_REQUEST")
			return
		}
		auth := strings.Fields(r.Header.Get("Authorization"))
		if len(r.Header.Values("Authorization")) != 1 || len(auth) != 2 || !strings.EqualFold(auth[0], "Bearer") {
			failure(401, "UNAUTHENTICATED")
			return
		}
		verification, _, err := admit(auth[1])
		if err != nil {
			failure(401, "UNAUTHENTICATED")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			failure(503, "OVERLOADED")
			return
		}
		runtime, err := noxmcp.NewSuite(noxmcp.Options{AppID: "nox-yard", Name: "nox-yard", Version: version, Timeout: Timeout, MaxInFlight: MaxInFlight, MaxPayloadBytes: noxmcp.SuiteMaxPayloadBytes, Tools: tools}, noxmcp.SuiteRuntimeOptions{
			Verification: func() noxmcp.SuiteVerificationOptions { return verification }, Policies: policies,
			AdditionalScopes: func(name string, args map[string]any, _ noxmcp.ExecutionContext) ([]string, error) {
				if name == "yard_project_configuration_read" && args["reveal"] == true {
					return []string{"nox:sensitive-read"}, nil
				}
				return nil, nil
			},
			IsActive: func(id *noxmcp.VerifiedSuiteIdentity) bool { _, _, err := admit(id.AccessToken()); return err == nil },
		})
		if err != nil {
			failure(503, "INTEGRATION_UNAVAILABLE")
			return
		}
		runtime.HTTPHandler().ServeHTTP(w, r)
	}), nil
}
