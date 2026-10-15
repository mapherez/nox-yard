package managed

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func TestAdoptionDirectoryRequiresReliableOriginalBase(t *testing.T) {
	for _, test := range []struct {
		name           string
		labels         []string
		supplied, want string
	}{
		{"metadata", []string{"/srv/apps/sample", "/srv/apps/sample"}, "", "/srv/apps/sample"},
		{"explicit fallback", []string{""}, "/srv/original", "/srv/original"},
		{"missing", []string{""}, "", ""},
		{"ambiguous", []string{"/srv/a", "/srv/b"}, "", ""},
		{"override", []string{"/srv/a"}, "/srv/b", ""},
		{"relative", []string{"relative"}, "", ""},
		{"root", []string{"/"}, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := make([]container.InspectResponse, len(test.labels))
			for i, label := range test.labels {
				items[i].Config = &container.Config{Labels: map[string]string{"com.docker.compose.project.working_dir": label}}
			}
			got, err := adoptionDirectory(items, test.supplied)
			if test.want == "" {
				if !errors.Is(err, ErrInvalidSource) {
					t.Fatalf("unsafe directory accepted: %q %v", got, err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
}

func adoptionSample(t *testing.T) (composeModel, container.InspectResponse, map[string]imageDefaults) {
	t.Helper()
	var model composeModel
	if err := json.Unmarshal([]byte(`{"services":{"web":{"image":"alpine:3.23","command":["sleep","600"],"environment":{"TOKEN":"secret-value"},"ports":[{"published":"8090","target":80,"protocol":"tcp"}],"volumes":[{"type":"bind","source":"./data","target":"/data"},{"type":"volume","source":"files","target":"/files","read_only":true}],"networks":{"default":{}},"stop_grace_period":"1s"}},"volumes":{"files":{"name":"sample_files"}},"networks":{"default":{"name":"sample_default"}}}`), &model); err != nil {
		t.Fatal(err)
	}
	item := readyContainer("web")
	item.Config.Labels["com.docker.compose.config-hash"] = "fixture-original"
	item.Config.Labels["com.docker.compose.oneoff"] = "False"
	item.Config.Labels["com.docker.compose.container-number"] = "1"
	item.Config.Image = "alpine:3.23"
	item.Config.Cmd = []string{"sleep", "600"}
	item.Config.Env = []string{"PATH=/bin", "TOKEN=secret-value"}
	item.Config.StopTimeout = new(1)
	item.HostConfig = &container.HostConfig{PortBindings: network.PortMap{network.MustParsePort("80/tcp"): {{HostPort: "8090"}}}}
	item.Mounts = []container.MountPoint{{Type: mount.TypeBind, Source: "/srv/original/data", Destination: "/data", RW: true}, {Type: mount.TypeVolume, Name: "sample_files", Destination: "/files", RW: false}}
	item.NetworkSettings = &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{"sample_default": {}}}
	defaults := map[string]imageDefaults{"alpine:3.23": {ID: "image-id", Config: container.Config{Env: []string{"PATH=/bin"}}}}
	return model, item, defaults
}

func TestAdoptionComparesRelativeBindsAndMaskedRuntimeChanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*container.InspectResponse)
		want   string
	}{
		{"same", func(*container.InspectResponse) {}, ""},
		{"image ID", func(c *container.InspectResponse) { c.Image = "old-image-id" }, "Image ID"},
		{"environment removed", func(c *container.InspectResponse) { c.Config.Env = append(c.Config.Env, "REMOVED=other-secret") }, "Environment"},
		{"command", func(c *container.InspectResponse) { c.Config.Cmd = []string{"echo", "secret-value"} }, "Command"},
		{"entrypoint", func(c *container.InspectResponse) { c.Config.Entrypoint = []string{"sh"} }, "Entrypoint"},
		{"extra port", func(c *container.InspectResponse) {
			c.HostConfig.PortBindings[network.MustParsePort("81/tcp")] = []network.PortBinding{{HostPort: "8091"}}
		}, "Published ports"},
		{"removed port", func(c *container.InspectResponse) { clear(c.HostConfig.PortBindings) }, "Published ports"},
		{"bind base", func(c *container.InspectResponse) { c.Mounts[0].Source = "/srv/wrong/data" }, "Mounts"},
		{"volume read only", func(c *container.InspectResponse) { c.Mounts[1].RW = true }, "Mounts"},
		{"network", func(c *container.InspectResponse) { c.NetworkSettings.Networks["extra"] = &network.EndpointSettings{} }, "Networks"},
		{"healthcheck", func(c *container.InspectResponse) {
			c.Config.Healthcheck = &container.HealthConfig{Test: []string{"CMD", "true"}}
		}, "Healthcheck"},
		{"restart policy", func(c *container.InspectResponse) { c.HostConfig.RestartPolicy.Name = "always" }, "Restart policy"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, item, defaults := adoptionSample(t)
			test.change(&item)
			changes, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(changes, ";")
			if strings.Contains(text, "secret-value") || strings.Contains(text, "other-secret") {
				t.Fatal("comparison leaked secrets")
			}
			if test.want == "" {
				if len(changes) > 0 {
					t.Fatal(changes)
				}
			} else if !strings.Contains(text, test.want) {
				t.Fatalf("missing %s: %v", test.want, changes)
			}
		})
	}
}

func TestAdoptionRejectsUnsupportedAndUninspectableConfiguration(t *testing.T) {
	model, item, defaults := adoptionSample(t)
	item.HostConfig.Privileged = true
	if _, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults); !errors.Is(err, ErrUnsupportedComparison) {
		t.Fatal("unsupported runtime accepted")
	}
	item.HostConfig.Privileged = false
	service := model.Services["web"]
	service.raw["security_opt"] = json.RawMessage(`["secret-value"]`)
	model.Services["web"] = service
	if _, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults); !errors.Is(err, ErrUnsupportedComparison) || strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "invalid Compose source") {
		t.Fatalf("unsupported source accepted or exposed: %v", err)
	}
	delete(service.raw, "security_opt")
	model.Services["web"] = service
	item.Config = nil
	if _, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults); !errors.Is(err, ErrInvalidSource) {
		t.Fatal("uninspectable runtime accepted")
	}
}

func TestAdoptionFingerprintIgnoresTransientStateButDetectsConfigurationAndReplacement(t *testing.T) {
	_, item, _ := adoptionSample(t)
	before := adoptionRuntimeFingerprint([]container.InspectResponse{item})
	item.State.Health = &container.Health{Status: container.Unhealthy}
	item.State.StartedAt = "later"
	if before != adoptionRuntimeFingerprint([]container.InspectResponse{item}) {
		t.Fatal("health or uptime invalidated adoption")
	}
	item.Config.Env = append(item.Config.Env, "CHANGED=secret-value")
	if before == adoptionRuntimeFingerprint([]container.InspectResponse{item}) {
		t.Fatal("config change retained fingerprint")
	}
	item.Config.Env = item.Config.Env[:2]
	item.ID = "replacement"
	if before == adoptionRuntimeFingerprint([]container.InspectResponse{item}) {
		t.Fatal("replacement retained fingerprint")
	}
}

func TestHealthcheckDefaultsAndPrivatePreviewModel(t *testing.T) {
	base := &container.HealthConfig{Test: []string{"CMD", "true"}, Timeout: time.Second}
	got, err := desiredHealthcheck(base, &modelHealthcheck{Interval: "2s", Retries: new(2)})
	if err != nil || got.Interval != 2*time.Second || got.Timeout != time.Second || got.Retries != 2 || !reflect.DeepEqual(got.Test, base.Test) {
		t.Fatalf("image defaults lost: %+v %v", got, err)
	}
	if base.Interval != 0 {
		t.Fatal("image defaults mutated")
	}
	model, _, _ := adoptionSample(t)
	encoded, _ := json.Marshal(ProjectPreview{Preview: Preview{model: model}, filesFingerprint: "private-files", runtimeFingerprint: "private-runtime"})
	for _, secret := range []string{"secret-value", "private-files", "private-runtime", "model"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public preview leaked %s", secret)
		}
	}
}

func TestAdoptionCannotChangeTheBaseOfExistingRelativeBinds(t *testing.T) {
	model, item, _ := adoptionSample(t)
	if err := verifyAdoptionBinds("/srv/original", model, []container.InspectResponse{item}); err != nil {
		t.Fatal(err)
	}
	if err := verifyAdoptionBinds("/srv/wrong", model, []container.InspectResponse{item}); !errors.Is(err, ErrInvalidSource) {
		t.Fatal("unverified relative bind base accepted")
	}
	item.Mounts = nil
	if err := verifyAdoptionBinds("/srv/original", model, []container.InspectResponse{item}); !errors.Is(err, ErrInvalidSource) {
		t.Fatal("new relative bind silently adopted")
	}
}

func TestAdoptionRequiresLabelsThatComposeWillRecognize(t *testing.T) {
	for _, key := range []string{"com.docker.compose.config-hash", "com.docker.compose.oneoff", "com.docker.compose.container-number"} {
		model, item, defaults := adoptionSample(t)
		delete(item.Config.Labels, key)
		if _, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults); !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("unrecognized service accepted without %s: %v", key, err)
		}
	}
}

func TestAdoptionComparesInit(t *testing.T) {
	for _, test := range []struct {
		name   string
		source *bool
		actual *bool
		drift  bool
	}{
		{"omitted", nil, nil, false},
		{"explicit false", new(false), new(false), false},
		{"false and engine default", new(false), nil, false},
		{"enabled", new(true), new(true), false},
		{"undeclared enabled", nil, new(true), true},
		{"disabled at runtime", new(true), new(false), true},
		{"missing at runtime", new(true), nil, true},
		{"enabled at runtime", new(false), new(true), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, item, defaults := adoptionSample(t)
			service := model.Services["web"]
			if test.source != nil {
				service.raw["init"], _ = json.Marshal(*test.source)
				service.Init = test.source
			}
			model.Services["web"] = service
			item.HostConfig.Init = test.actual
			changes, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults)
			if err != nil || (len(changes) > 0) != test.drift || (test.drift && !strings.Contains(strings.Join(changes, ";"), "Init differs for web")) {
				t.Fatalf("unexpected init comparison: %v, %v", changes, err)
			}
		})
	}
}

func TestAdoptionComparesLoggingOverridesAndAllowsDaemonDefaults(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		actual container.LogConfig
		want   string
	}{
		{"omitted", "null", container.LogConfig{Type: "json-file"}, ""},
		{"inherited", "null", container.LogConfig{Type: "local", Config: map[string]string{"max-size": "10m"}}, ""},
		{"empty", `{}`, container.LogConfig{Type: "local", Config: map[string]string{"max-size": "10m"}}, ""},
		{"driver", `{"driver":"local"}`, container.LogConfig{Type: "local"}, ""},
		{"none", `{"driver":"none"}`, container.LogConfig{Type: "none"}, ""},
		{"options", `{"driver":"json-file","options":{"max-size":"10m","max-file":"3"}}`, container.LogConfig{Type: "json-file", Config: map[string]string{"max-file": "3", "max-size": "10m"}}, ""},
		{"inherited driver", `{"options":{"max-size":"10m"}}`, container.LogConfig{Type: "local", Config: map[string]string{"max-size": "10m"}}, ""},
		{"additional daemon option", `{"driver":"json-file","options":{"max-size":"10m"}}`, container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}}, ""},
		{"changed driver", `{"driver":"local"}`, container.LogConfig{Type: "json-file"}, "Logging driver"},
		{"changed option", `{"options":{"tag":"secret-value"}}`, container.LogConfig{Type: "json-file", Config: map[string]string{"tag": "other-secret"}}, "Logging options"},
		{"missing option", `{"options":{"max-size":"10m"}}`, container.LogConfig{Type: "json-file"}, "Logging options"},
		{"missing empty option", `{"options":{"tag":""}}`, container.LogConfig{Type: "json-file"}, "Logging options"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, item, defaults := adoptionSample(t)
			service := model.Services["web"]
			service.raw["logging"] = json.RawMessage(test.source)
			encoded, _ := json.Marshal(service.raw)
			if err := json.Unmarshal(encoded, &service); err != nil {
				t.Fatal(err)
			}
			model.Services["web"] = service
			item.HostConfig.LogConfig = test.actual
			changes, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults)
			text := strings.Join(changes, ";")
			if err != nil || (len(changes) == 0) != (test.want == "") || !strings.Contains(text, test.want) {
				t.Fatalf("unexpected logging comparison: %v, %v", changes, err)
			}
			if strings.Contains(text, "secret-value") || strings.Contains(text, "other-secret") {
				t.Fatal("logging options leaked in differences")
			}
		})
	}
}

func TestAdoptionUnsupportedComparisonReportsFieldAndService(t *testing.T) {
	for _, field := range []string{"security_opt", "cap_add", "cpus", "container_name"} {
		model, item, defaults := adoptionSample(t)
		service := model.Services["web"]
		service.raw[field] = json.RawMessage(`"secret-value"`)
		model.Services["web"] = service
		_, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults)
		if !errors.Is(err, ErrUnsupportedComparison) || errors.Is(err, ErrInvalidSource) || !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "service web") || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("unsupported field has incorrect diagnostic: %v", err)
		}
	}
}
