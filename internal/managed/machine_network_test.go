package managed

import (
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
	"gopkg.in/yaml.v3"
)

func TestNetworkPreparationPreservesProjectAndUpdatesPersistentPorts(t *testing.T) {
	before := FileSnapshot{Directory: "/srv/yard", ComposeFiles: []string{"compose.yaml"}, Files: map[string]string{
		"compose.yaml": "services:\n  nox-yard:\n    image: yard:fixture\n    ports: ['8095:8080']\n    environment: ['NOX_DATA_DIR=/data', 'SECRET=private-value']\n    volumes: ['./data:/data']\n  other:\n    image: alpine\n    ports: ['9090:90']\n",
		".env":         "# keep this\nOTHER=value\nNOX_PORT=8095\n",
	}}
	network := store.MachineNetwork{Yard: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 18095}, MCP: store.NetworkEndpoint{Bind: "0.0.0.0", Port: 18096}}
	after, err := prepareNetworkFiles(before, network)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := yaml.Unmarshal([]byte(after.Files["compose.yaml"]), &root); err != nil {
		t.Fatal(err)
	}
	services := root["services"].(map[string]any)
	yard := services["nox-yard"].(map[string]any)
	if len(yard["ports"].([]any)) != 2 || len(services["other"].(map[string]any)["ports"].([]any)) != 1 {
		t.Fatal("ports changed outside Yard")
	}
	env := yard["environment"].(map[string]any)
	if env["SECRET"] != "private-value" || env["NOX_MCP_LISTEN_ADDR"] != ":8081" {
		t.Fatal("environment lost")
	}
	if !strings.Contains(after.Files[".env"], "OTHER=value") || !strings.Contains(after.Files[".env"], "NOX_MCP_PORT") || !strings.Contains(after.Files[".env"], "18096") {
		t.Fatal("persistent dotenv not updated")
	}
	if before.Files["compose.yaml"] == after.Files["compose.yaml"] || before.Files[".env"] == after.Files[".env"] || strings.Contains(before.Files[".env"], "18096") {
		t.Fatal("original snapshot changed")
	}
}
func TestNetworkPreparationRejectsAmbiguousPortMerges(t *testing.T) {
	before := FileSnapshot{ComposeFiles: []string{"a.yaml", "b.yaml"}, Files: map[string]string{"a.yaml": "services: {nox-yard: {ports: ['8095:8080']}}", "b.yaml": "services: {nox-yard: {ports: ['8096:8081']}}"}}
	network := store.MachineNetwork{Yard: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8095}, MCP: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8096}}
	if _, err := prepareNetworkFiles(before, network); err == nil {
		t.Fatal("ambiguous merge accepted")
	}
}
