package store

import (
	"strings"
	"sync"
	"testing"
)

func machineFixture(t *testing.T) *Store {
	t.Helper()
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	err = data.InitializeMachine(MachineNetwork{Yard: NetworkEndpoint{Bind: "127.0.0.1", Port: 8095}, MCP: NetworkEndpoint{Bind: "127.0.0.1", Port: 8096}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestMachineRotationRevocationAndBoundedHistory(t *testing.T) {
	data := machineFixture(t)
	settings, err := data.MachineSettings()
	if err != nil || settings.MCPEnabled || settings.APIEnabled || settings.Write || settings.Secrets || settings.AuthMode != "token" {
		t.Fatalf("unsafe defaults: %+v %v", settings, err)
	}
	var previous string
	for i := 0; i < 14; i++ {
		token, err := data.RotateMachineToken("owner", false)
		if err != nil {
			t.Fatal(err)
		}
		if valid, _ := data.ValidateMachineToken(token); !valid {
			t.Fatal("new token rejected")
		}
		if valid, _ := data.ValidateMachineToken(previous); valid {
			t.Fatal("old token survived rotation")
		}
		var raw, digest string
		if err := data.db.QueryRow(`SELECT settings,token_digest FROM machine_settings WHERE id=1`).Scan(&raw, &digest); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, token) || digest == token {
			t.Fatal("raw token persisted")
		}
		previous = token
	}
	events, err := data.MachineTokenHistory()
	if err != nil || len(events) != 10 || events[0].Action != "replaced" || events[0].Actor != "owner" || events[0].CreatedAt == 0 {
		t.Fatalf("history: %+v %v", events, err)
	}
	before, _ := data.MachineSettings()
	if _, err := data.RotateMachineToken("owner", true); err != nil {
		t.Fatal(err)
	}
	after, _ := data.MachineSettings()
	if after.HasToken || after.TokenCreatedAt != before.TokenCreatedAt {
		t.Fatal("revocation lost generation history")
	}
	if valid, _ := data.ValidateMachineToken(previous); valid {
		t.Fatal("revoked token survived")
	}
}
func TestMachineConcurrentRotationHasOneWinner(t *testing.T) {
	data := machineFixture(t)
	tokens := make(chan string, 6)
	failures := make(chan error, 6)
	var group sync.WaitGroup
	for i := 0; i < 6; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			token, err := data.RotateMachineToken("owner", false)
			tokens <- token
			failures <- err
		}()
	}
	group.Wait()
	close(tokens)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	validCount := 0
	for token := range tokens {
		if valid, _ := data.ValidateMachineToken(token); valid {
			validCount++
		}
	}
	if validCount != 1 {
		t.Fatalf("active tokens: %d", validCount)
	}
}
func TestMachineLegacyKeyNeverReimportedAndSettingsValidated(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	network := MachineNetwork{Yard: NetworkEndpoint{Bind: "127.0.0.1", Port: 8095}, MCP: NetworkEndpoint{Bind: "127.0.0.1", Port: 8096}}
	if err := data.InitializeMachine(network, true, "legacy-token"); err != nil {
		t.Fatal(err)
	}
	if valid, _ := data.ValidateMachineToken("legacy-token"); !valid {
		t.Fatal("legacy key not imported")
	}
	if _, err := data.RotateMachineToken("owner", true); err != nil {
		t.Fatal(err)
	}
	if err := data.InitializeMachine(network, true, "legacy-token"); err != nil {
		t.Fatal(err)
	}
	if valid, _ := data.ValidateMachineToken("legacy-token"); valid {
		t.Fatal("stale environment key resurrected")
	}
	settings, _ := data.MachineSettings()
	settings.MCPEnabled = true
	if _, err := data.SaveMachineSettings(settings); err == nil {
		t.Fatal("enabled token mode without token")
	}
	settings.AuthMode = "anonymous"
	settings.APIEnabled = false
	saved, err := data.SaveMachineSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.SaveMachineSettings(settings); err != ErrJobChanged {
		t.Fatal("stale revision accepted")
	}
	saved.Network.MCP.Port = saved.Network.Yard.Port
	if _, err := data.SaveMachineSettings(saved); err == nil {
		t.Fatal("port conflict accepted")
	}
}
