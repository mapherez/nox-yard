package managed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/suite"
	"gopkg.in/yaml.v3"
)

const suiteProductLabel = "io.nox.suite.product-id"
const suiteMount = "/run/nox-suite"

func yamlField(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
func yamlSet(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
func yamlMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }
func yamlScalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
func claimedSuiteService(service *yaml.Node) bool {
	labels := yamlField(service, "labels")
	if labels == nil {
		return false
	}
	if labels.Kind == yaml.MappingNode {
		return yamlField(labels, suiteProductLabel) != nil
	}
	for _, n := range labels.Content {
		if strings.HasPrefix(n.Value, suiteProductLabel+"=") {
			return true
		}
	}
	return false
}

// Only explicitly marked participating services receive the shared volume.
// Unknown YAML fields/comments and all ordinary Compose sources remain intact.
// The marker selects a candidate; only Backend proof/registration grants trust.
func prepareSuiteSource(content, volume string) (string, error) {
	if volume == "" || !strings.Contains(content, suiteProductLabel) {
		return content, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return "", ErrInvalidSource
	}
	if len(document.Content) != 1 {
		return "", ErrInvalidSource
	}
	root := document.Content[0]
	services := yamlField(root, "services")
	if services == nil {
		return content, nil
	}
	changed := false
	for i := 1; i < len(services.Content); i += 2 {
		service := services.Content[i]
		if !claimedSuiteService(service) {
			continue
		}
		mounts := yamlField(service, "volumes")
		if mounts == nil {
			mounts = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			yamlSet(service, "volumes", mounts)
		}
		if mounts.Kind != yaml.SequenceNode {
			return "", fmt.Errorf("%w: Suite service volumes must be a list", ErrInvalidSource)
		}
		found := false
		for _, mount := range mounts.Content {
			if mount.Kind == yaml.MappingNode {
				target := yamlField(mount, "target")
				if target != nil && target.Value == suiteMount {
					found = true
					break
				}
			}
			if mount.Kind == yaml.ScalarNode {
				parts := strings.Split(mount.Value, ":")
				if len(parts) > 1 && parts[1] == suiteMount {
					found = true
					break
				}
			}
		}
		if found {
			continue
		}
		volumes := yamlField(root, "volumes")
		if volumes == nil || volumes.Tag == "!!null" {
			volumes = yamlMap()
			yamlSet(root, "volumes", volumes)
		}
		resource := "nox-suite-local"
		if existing := yamlField(volumes, resource); existing != nil {
			name := yamlField(existing, "name")
			if name == nil || name.Value != volume {
				return "", fmt.Errorf("%w: the Suite volume conflicts with the local channel", ErrInvalidSource)
			}
		} else {
			definition := yamlMap()
			yamlSet(definition, "name", yamlScalar(volume))
			yamlSet(definition, "external", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			yamlSet(volumes, resource, definition)
		}
		mounts.Content = append(mounts.Content, yamlScalar(resource+":"+suiteMount))
		changed = true
	}
	if !changed {
		return content, nil
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}
func suiteTargets(preview Preview, volume string) ([]store.SuiteTarget, error) {
	out := []store.SuiteTarget{}
	seen := map[string]bool{}
	names := []string{}
	for name := range preview.model.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		service := preview.model.Services[name]
		if preview.model.ActiveServices != nil && !slices.Contains(preview.model.ActiveServices, name) {
			continue
		}
		product, claims := service.Labels[suiteProductLabel]
		if !claims {
			continue
		}
		if !noxmcp.ValidSuiteProductID(product) || seen[product] {
			return nil, fmt.Errorf("%w: Suite product labels must be valid and unique in the project", ErrInvalidSource)
		}
		replicas := 1
		if service.Scale != nil {
			replicas = *service.Scale
		}
		if service.Deploy.Replicas != nil {
			replicas = *service.Deploy.Replicas
		}
		if replicas != 1 {
			return nil, fmt.Errorf("%w: a Suite product requires one participating service replica", ErrInvalidSource)
		}
		if volume != "" {
			valid := false
			for _, mount := range service.Volumes {
				if mount.Target != suiteMount {
					continue
				}
				resource, ok := preview.model.Volumes[mount.Source]
				valid = ok && mount.Type == "volume" && !mount.ReadOnly && resource.Name == volume
			}
			if !valid {
				return nil, fmt.Errorf("%w: a Suite service requires the shared local volume at /run/nox-suite", ErrInvalidSource)
			}
		}
		seen[product] = true
		out = append(out, store.SuiteTarget{ProductID: product, Service: name})
	}
	return out, nil
}

// Inspect reuses the existing Docker inspection source. Labels select the exact
// participating service; a web URL or another container is not product identity.
func (m *Manager) Inspect(ctx context.Context, projectID, service, product string) (suite.Deployment, error) {
	var out suite.Deployment
	project, ok := strings.CutPrefix(projectID, "compose:")
	if !ok || project == "" {
		return out, errors.New("invalid Suite deployment project")
	}
	containers, err := m.runtime(ctx, project)
	if err != nil {
		return out, err
	}
	for _, item := range containers {
		if item.Config == nil || item.Config.Labels["com.docker.compose.service"] != service || item.Config.Labels[suiteProductLabel] != product {
			continue
		}
		if out.Present {
			return suite.Deployment{}, errors.New("Suite service has multiple containers")
		}
		out.Present = true
		out.ContainerIDs = []string{item.ID}
		out.State = "unknown"
		if item.State != nil {
			switch {
			case item.State.Restarting:
				out.State = "starting"
			case item.State.Paused:
				out.State = "stopped"
			case item.State.Running:
				out.State = "running"
			default:
				out.State = "stopped"
			}
		}
	}
	return out, nil
}

// ConfigureSuiteVolume is called once at process startup after the actual local
// channel is resolved. Ordinary standalone managers keep Suite preparation off.
func (m *Manager) ConfigureSuiteVolume(volume string) {
	if volume == "" {
		volume = "nox-suite-local"
	}
	m.suiteVolume = volume
}
