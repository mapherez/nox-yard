package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"gopkg.in/yaml.v3"
)

type MachineNetworkStatus struct {
	Available bool                 `json:"available"`
	Pending   bool                 `json:"pending"`
	Applied   store.MachineNetwork `json:"applied"`
	Job       *store.Job           `json:"job,omitempty"`
}
type networkPlan struct {
	Before   FileSnapshot
	After    FileSnapshot
	Runtime  []container.InspectResponse
	Network  store.MachineNetwork
	Revision int64
	Mutating bool
}

func machineSelf(ctx context.Context) (container.InspectResponse, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return container.InspectResponse{}, err
	}
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return container.InspectResponse{}, err
	}
	defer cli.Close()
	result, err := cli.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}
	item := result.Container
	if !strings.HasPrefix(item.ID, hostname) || item.Config == nil || item.HostConfig == nil || item.Config.Labels["com.docker.compose.service"] != "nox-yard" || item.Config.Labels["com.docker.compose.project"] == "" || item.HostConfig.NetworkMode == "host" {
		return item, errors.New("Network application requires Yard's Docker Compose service.")
	}
	return item, nil
}
func networkOf(item container.InspectResponse) store.MachineNetwork {
	endpoint := func(port string) store.NetworkEndpoint {
		if item.NetworkSettings == nil {
			return store.NetworkEndpoint{}
		}
		for p, bindings := range item.NetworkSettings.Ports {
			if p.String() == port {
				for _, binding := range bindings {
					if binding.HostIP.IsValid() && !binding.HostIP.Is4() {
						continue
					}
					n, _ := strconv.Atoi(binding.HostPort)
					bind := "0.0.0.0"
					if binding.HostIP.IsValid() {
						bind = binding.HostIP.String()
					}
					return store.NetworkEndpoint{Bind: bind, Port: n}
				}
			}
		}
		return store.NetworkEndpoint{}
	}
	return store.MachineNetwork{Yard: endpoint("8080/tcp"), MCP: endpoint("8081/tcp")}
}
func DetectMachineNetwork(ctx context.Context) (store.MachineNetwork, error) {
	item, err := machineSelf(ctx)
	return networkOf(item), err
}

func (m *Manager) MachineNetworkStatus(ctx context.Context) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	item, err := machineSelf(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := m.data.MachineSettings()
	if err != nil {
		return nil, err
	}
	applied := networkOf(item)
	status := MachineNetworkStatus{Available: true, Applied: applied, Pending: applied != settings.Network}
	recent, err := m.data.Jobs(ctx, "yard:self", false)
	if err != nil {
		return nil, err
	}
	for _, job := range recent {
		if job.Domain == "network" {
			status.Job = &job
			break
		}
	}
	return status, nil
}

// prepareNetworkFiles keeps original Compose files and future host invocations
// consistent. Ambiguous multi-file port merges are refused before any write.
func prepareNetworkFiles(before FileSnapshot, network store.MachineNetwork) (FileSnapshot, error) {
	if err := network.Validate(); err != nil {
		return before, err
	}
	after := before
	after.Files = map[string]string{}
	for name, value := range before.Files {
		after.Files[name] = value
	}
	var chosen *yaml.Node
	documents := map[string]*yaml.Node{}
	portDefinitions := 0
	for _, name := range before.ComposeFiles {
		var doc yaml.Node
		if yaml.Unmarshal([]byte(before.Files[name]), &doc) != nil || len(doc.Content) != 1 {
			return after, ErrInvalidSource
		}
		service := yamlMappingValue(yamlMappingValue(doc.Content[0], "services"), "nox-yard")
		if service == nil {
			continue
		}
		if service.Kind != yaml.MappingNode {
			return after, ErrInvalidSource
		}
		if yamlMappingValue(service, "ports") != nil {
			portDefinitions++
		}
		chosen = service
		documents[name] = &doc
	}
	if chosen == nil || portDefinitions > 1 {
		return after, errors.New("Use a single Yard ports definition before applying network settings.")
	}
	// Remove an earlier ports definition to prevent Compose sequence merging.
	for _, doc := range documents {
		service := yamlMappingValue(yamlMappingValue(doc.Content[0], "services"), "nox-yard")
		for i := 0; i < len(service.Content); i += 2 {
			if service.Content[i].Value == "ports" {
				service.Content = append(service.Content[:i], service.Content[i+2:]...)
				break
			}
		}
	}
	setNode := func(parent *yaml.Node, key string, value *yaml.Node) {
		for i := 0; i < len(parent.Content); i += 2 {
			if parent.Content[i].Value == key {
				parent.Content[i+1] = value
				return
			}
		}
		parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	}
	ports := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range []string{"${NOX_BIND_ADDRESS:-127.0.0.1}:${NOX_PORT:-8095}:8080", "${NOX_MCP_BIND_ADDRESS:-127.0.0.1}:${NOX_MCP_PORT:-8096}:8081"} {
		ports.Content = append(ports.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: yaml.DoubleQuotedStyle})
	}
	setNode(chosen, "ports", ports)
	env := yamlMappingValue(chosen, "environment")
	if env == nil {
		env = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setNode(chosen, "environment", env)
	}
	if env.Kind == yaml.SequenceNode {
		converted := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, value := range env.Content {
			key, v, has := strings.Cut(value.Value, "=")
			node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
			if has {
				node.Tag, node.Value = "!!str", v
			}
			setNode(converted, key, node)
		}
		env = converted
		setNode(chosen, "environment", env)
	}
	if env.Kind != yaml.MappingNode {
		return after, ErrInvalidSource
	}
	setNode(env, "NOX_LISTEN_ADDR", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ":8080"})
	setNode(env, "NOX_MCP_LISTEN_ADDR", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ":8081"})
	for name, doc := range documents {
		encoded, err := yaml.Marshal(doc)
		if err != nil {
			return after, err
		}
		after.Files[name] = string(encoded)
	}
	for key, value := range map[string]string{"NOX_BIND_ADDRESS": network.Yard.Bind, "NOX_PORT": strconv.Itoa(network.Yard.Port), "NOX_MCP_BIND_ADDRESS": network.MCP.Bind, "NOX_MCP_PORT": strconv.Itoa(network.MCP.Port)} {
		after.Files[".env"] = setDotenvValue(after.Files[".env"], key, value)
	}
	return after, nil
}

func (m *Manager) ApplyMachineNetwork(ctx context.Context, revision int64) (store.Job, error) {
	settings, err := m.data.MachineSettings()
	if err != nil {
		return store.Job{}, err
	}
	if settings.Revision != revision {
		return store.Job{}, store.ErrJobChanged
	}
	self, err := machineSelf(ctx)
	if err != nil {
		return store.Job{}, errors.New("Network changes require Yard running in Docker Compose.")
	}
	if networkOf(self) == settings.Network {
		return store.Job{}, nil
	}
	name := self.Config.Labels["com.docker.compose.project"]
	before, err := m.readFileSnapshot(ctx, name)
	if err != nil {
		return store.Job{}, errors.New("Yard's original Compose files must be accessible before applying network settings.")
	}
	for _, port := range filePreview(before).model.Services["nox-yard"].Ports {
		if port.Target != 8080 && port.Target != 8081 {
			return store.Job{}, errors.New("Additional Yard service ports require manual Compose configuration.")
		}
	}
	after, err := prepareNetworkFiles(before, settings.Network)
	if err != nil {
		return store.Job{}, err
	}
	if err := resolveFileSnapshot(ctx, &after); err != nil {
		return store.Job{}, err
	}
	health := filePreview(after).model.Services["nox-yard"].Healthcheck
	if health == nil || health.Disable || !strings.Contains(strings.Join(health.Test, " "), "http://127.0.0.1:8080/healthz") {
		return store.Job{}, errors.New("Network recreation requires Yard's /healthz Docker healthcheck.")
	}
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return store.Job{}, err
	}
	if err := validateSelfFileConfiguration(filePreview(after), runtime, m.data.Directory()); err != nil {
		return store.Job{}, err
	}
	if err := m.assessServicePorts(ctx, name, "nox-yard", scopedFilePreview(after, "nox-yard").model); err != nil {
		return store.Job{}, err
	}
	job, err := store.NewJob("compose:"+name, "network", "apply", []string{"yard:self", "container:" + self.ID})
	if err != nil {
		return job, err
	}
	job.ProjectName = name
	plan := networkPlan{Before: before, After: after, Runtime: serviceRuntime(runtime, "nox-yard"), Network: settings.Network, Revision: revision}
	encoded, _ := json.Marshal(plan)
	job.Payload = string(encoded)
	// Reserve first, then recheck the settings revision before launching.
	if err := m.data.CreateJob(job); err != nil {
		return job, err
	}
	current, err := m.data.MachineSettings()
	if err != nil || current.Revision != revision {
		_ = m.data.FinishJob(job.ID, job.Owner, "failed", "failed", "Settings changed before application.", "")
		return job, store.ErrJobChanged
	}
	if err := m.data.SetFileConfiguration(configurationLocation(before)); err != nil {
		_ = m.data.FinishJob(job.ID, job.Owner, "failed", "failed", "Cannot persist Compose file locations.", "")
		return job, err
	}
	launch := m.launch
	if launch == nil {
		launch = jobs.Launch
	}
	if err := launch(ctx, m.data, job, "network-worker"); err != nil {
		outcome := "failed"
		if errors.Is(err, jobs.ErrLaunchUncertain) {
			outcome = "recovery_required"
		}
		_ = m.data.FinishJob(job.ID, job.Owner, "failed", outcome, "Network worker could not be launched.", "")
		return job, errors.New("Network worker could not be launched; inspect operation history.")
	}
	return job, nil
}

func RunNetworkWorker(data *store.Store, id, owner string) (result error) {
	job, found, err := data.Job(id)
	if err != nil {
		return err
	}
	if !found || job.Domain != "network" {
		return store.ErrJobChanged
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(job.DeadlineAt, 0))
	defer cancel()
	if err := jobs.ValidateWorker(ctx, job, owner); err != nil {
		return err
	}
	if err := data.ClaimJob(id, owner); err != nil {
		return err
	}
	var plan networkPlan
	if err := json.Unmarshal([]byte(job.Payload), &plan); err != nil {
		return err
	}
	m, _ := NewManager(data, nil)
	mutated, completed := false, false
	failureMessage := "Network application failed. The requested settings remain pending."
	defer func() {
		if recover() != nil {
			result = errors.New("Network worker failed unexpectedly.")
		}
		if completed {
			return
		}
		outcome, rollback := "failed", "not_needed"
		message := failureMessage
		if mutated {
			recovery, stop := context.WithTimeout(context.Background(), 3*time.Minute)
			defer stop()
			state, stateErr := jobs.State(recovery, job)
			if stateErr != nil || state.HelpersRunning {
				outcome, rollback = "recovery_required", "not_performed"
			} else {
				fileErr := saveConfigurationFiles(recovery, plan.After, plan.Before)
				runtimeErr := recreate.RestoreOperationRuntime(recovery, job.ProjectName, "nox-yard", plan.Runtime, []string{"nox-yard"}, job.ID)
				if runtimeErr == nil {
					runtimeErr = m.markObservedSource(recovery, job.ProjectName)
				}
				if fileErr != nil || runtimeErr != nil {
					outcome, rollback = "recovery_required", "failed"
					message += " Inspect retained job data and host files before recovery."
				} else {
					outcome, rollback = "rolled_back", "restored"
				}
			}
		}
		if err := data.FinishJob(id, owner, "failed", outcome, message, rollback); err != nil {
			result = err
		}
	}()
	current, err := m.readFileSnapshot(ctx, job.ProjectName)
	if err != nil {
		failureMessage = "Current host configuration could not be inspected; no network changes were applied."
		return fmt.Errorf("inspect host configuration before network application: %w", err)
	}
	if current.Fingerprint != plan.Before.Fingerprint {
		failureMessage = "Host configuration changed before application; review the current files. No network changes were applied."
		return errors.New("Host files changed before application.")
	}
	runtime, err := m.runtime(ctx, job.ProjectName)
	if err != nil {
		failureMessage = "The existing Yard container could not be inspected; no network changes were applied."
		return fmt.Errorf("inspect Yard before network application: %w", err)
	}
	if adoptionRuntimeFingerprint(serviceRuntime(runtime, "nox-yard")) != adoptionRuntimeFingerprint(plan.Runtime) {
		failureMessage = "The existing Yard container changed before application; review it before retrying. No network changes were applied."
		return store.ErrJobChanged
	}
	if err := data.JobProgress(id, owner, "saving_configuration", nil, nil); err != nil {
		return err
	}
	plan.Mutating = true
	encoded, _ := json.Marshal(plan)
	if err := data.JobPayload(id, owner, string(encoded)); err != nil {
		return err
	}
	mutated = true
	if err := saveConfigurationFiles(ctx, plan.Before, plan.After); err != nil {
		return err
	}
	preview := scopedFilePreview(plan.After, "nox-yard")
	if len(plan.Runtime) != 1 {
		return ErrInvalidSource
	}
	definition, err := resolvedDefinition(preview, plan.After.Directory, map[string]string{"nox-yard": plan.Runtime[0].Image})
	if err != nil {
		return err
	}
	definition, err = definitionOperation(definition, job.ID)
	if err != nil {
		return err
	}
	if err := data.JobProgress(id, owner, "replacing", nil, nil); err != nil {
		return err
	}
	if err := runResolvedCompose(ctx, job.ProjectName, plan.After.Directory, definition, "up", "-d", "--force-recreate", "--no-deps", "--no-build", "--pull", "never", "nox-yard"); err != nil {
		return errors.New("Network recreation failed.")
	}
	if err := data.JobProgress(id, owner, "verifying", nil, nil); err != nil {
		return err
	}
	if err := m.verify(ctx, job.ProjectName, preview); err != nil {
		return errors.New("Yard health verification failed.")
	}
	items, err := m.runtime(ctx, job.ProjectName)
	if err != nil {
		return err
	}
	verified := false
	for _, item := range serviceRuntime(items, "nox-yard") {
		if networkOf(item) == plan.Network && item.State != nil && item.State.Health != nil && item.State.Health.Status == "healthy" {
			verified = true
		}
	}
	if !verified {
		return fmt.Errorf("published ports or /healthz verification failed")
	}
	if err := m.markObservedSource(ctx, job.ProjectName); err != nil {
		return err
	}
	if err := data.FinishJob(id, owner, "succeeded", "applied", "", ""); err != nil {
		return err
	}
	completed = true
	return nil
}
