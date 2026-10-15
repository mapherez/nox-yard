package managed

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func tmpfsSample(t *testing.T) (composeModel, container.InspectResponse, map[string]imageDefaults) {
	t.Helper()
	model, item, defaults := adoptionSample(t)
	service := model.Services["web"]
	service.raw["tmpfs"] = json.RawMessage(`["/data","/run:size=64m,mode=1770"]`)
	// Include Compose's two byte-size serialization forms and default options.
	service.raw["volumes"] = json.RawMessage(`[
		{"type":"tmpfs","target":"/cache","read_only":true,"tmpfs":{"size":"65536","mode":448}},
		{"type":"tmpfs","target":"/work","tmpfs":{"size":4096}},
		{"type":"tmpfs","target":"/empty"}
	]`)
	encoded, _ := json.Marshal(service.raw)
	if err := json.Unmarshal(encoded, &service); err != nil {
		t.Fatal(err)
	}
	model.Services["web"] = service
	item.HostConfig.Tmpfs = map[string]string{"/data": "", "/run": "size=64m,mode=1770"}
	item.HostConfig.Mounts = []mount.Mount{
		{Type: mount.TypeTmpfs, Target: "/cache", ReadOnly: true, TmpfsOptions: &mount.TmpfsOptions{SizeBytes: 65536, Mode: 0700}},
		{Type: mount.TypeTmpfs, Target: "/work", TmpfsOptions: &mount.TmpfsOptions{SizeBytes: 4096}},
		{Type: mount.TypeTmpfs, Target: "/empty"},
	}
	// Engine can omit tmpfs from Inspect.Mounts, including for running containers.
	item.Mounts = nil
	return model, item, defaults
}

func TestAdoptionComparesTmpfsConfigurationWithoutInspectMounts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*container.InspectResponse)
		drift  bool
	}{
		{"same", func(*container.InspectResponse) {}, false},
		{"inspect includes tmpfs", func(c *container.InspectResponse) {
			c.Mounts = []container.MountPoint{{Type: mount.TypeTmpfs, Destination: "/data", Mode: "rw,nosuid,nodev"}}
		}, false},
		{"missing short", func(c *container.InspectResponse) { delete(c.HostConfig.Tmpfs, "/data") }, true},
		{"extra short", func(c *container.InspectResponse) { c.HostConfig.Tmpfs["/other"] = "" }, true},
		{"changed short options", func(c *container.InspectResponse) { c.HostConfig.Tmpfs["/run"] = "size=32m,mode=1770" }, true},
		{"changed option order", func(c *container.InspectResponse) { c.HostConfig.Tmpfs["/run"] = "mode=1770,size=64m" }, true},
		{"missing long", func(c *container.InspectResponse) { c.HostConfig.Mounts = c.HostConfig.Mounts[1:] }, true},
		{"extra long", func(c *container.InspectResponse) {
			c.HostConfig.Mounts = append(c.HostConfig.Mounts, mount.Mount{Type: mount.TypeTmpfs, Target: "/other"})
		}, true},
		{"size", func(c *container.InspectResponse) { c.HostConfig.Mounts[0].TmpfsOptions.SizeBytes = 32768 }, true},
		{"mode", func(c *container.InspectResponse) { c.HostConfig.Mounts[0].TmpfsOptions.Mode = 0777 }, true},
		{"read only", func(c *container.InspectResponse) { c.HostConfig.Mounts[0].ReadOnly = false }, true},
		{"target", func(c *container.InspectResponse) { c.HostConfig.Mounts[0].Target = "/other" }, true},
		{"empty options", func(c *container.InspectResponse) { c.HostConfig.Mounts[2].TmpfsOptions = &mount.TmpfsOptions{} }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, item, defaults := tmpfsSample(t)
			test.change(&item)
			changes, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults)
			if err != nil || (len(changes) > 0) != test.drift || (test.drift && strings.Join(changes, ";") != "Tmpfs mounts differs for web") {
				t.Fatalf("unexpected tmpfs comparison: %v, %v", changes, err)
			}
		})
	}
}

func TestAdoptionRejectsUndeclaredTmpfsAndUnsupportedOptions(t *testing.T) {
	model, item, defaults := adoptionSample(t)
	item.HostConfig.Tmpfs = map[string]string{"/data": "secret-value"}
	changes, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults)
	if err != nil || !strings.Contains(strings.Join(changes, ";"), "Tmpfs mounts") || strings.Contains(strings.Join(changes, ";"), "secret-value") {
		t.Fatalf("undeclared tmpfs accepted or values exposed: %v, %v", changes, err)
	}
	model, item, defaults = tmpfsSample(t)
	item.HostConfig.Mounts[0].TmpfsOptions.Options = [][]string{{"uid", "secret-value"}}
	if _, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults); !errors.Is(err, ErrUnsupportedComparison) || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("unsupported tmpfs options accepted or values exposed: %v", err)
	}
	model, item, defaults = tmpfsSample(t)
	service := model.Services["web"]
	service.raw["volumes"] = json.RawMessage(`[{"type":"tmpfs","target":"/cache","tmpfs":{"uid":"secret-value"}}]`)
	model.Services["web"] = service
	if _, err := compareAdoption("sample", "/srv/original", model, []container.InspectResponse{item}, defaults); !errors.Is(err, ErrUnsupportedComparison) || !strings.Contains(err.Error(), "tmpfs.uid") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("unknown source tmpfs option accepted or values exposed: %v", err)
	}
}

func TestResolvedDefinitionPreservesTmpfsForRollback(t *testing.T) {
	raw := `{"services":{"web":{"image":"moving:tag","tmpfs":["/data","/run:size=64m,mode=1770"],"volumes":[{"type":"tmpfs","target":"/cache","read_only":true,"tmpfs":{"size":"65536","mode":448}}]}}}`
	definition, err := resolvedDefinition(Preview{resolved: json.RawMessage(raw)}, "/host/original", map[string]string{"web": "sha256:old"})
	if err != nil {
		t.Fatal(err)
	}
	var model composeModel
	if err := json.Unmarshal([]byte(definition), &model); err != nil {
		t.Fatal(err)
	}
	host := &container.HostConfig{Tmpfs: map[string]string{"/data": "", "/run": "size=64m,mode=1770"}, Mounts: []mount.Mount{{Type: mount.TypeTmpfs, Target: "/cache", ReadOnly: true, TmpfsOptions: &mount.TmpfsOptions{SizeBytes: 65536, Mode: 0700}}}}
	if !matchesTmpfs(model.Services["web"], host) || model.Services["web"].Image != "sha256:old" {
		t.Fatal("rollback definition changed tmpfs configuration")
	}
}
