package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var projectNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
var interpolationPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)([^}]*)\}`)
var variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Variable struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
}

type Service struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Ports    []string `json:"ports"`
	Volumes  []string `json:"volumes"`
	Networks []string `json:"networks"`
}

type Preview struct {
	Name        string    `json:"name"`
	Services    []Service `json:"services"`
	Volumes     []string  `json:"volumes"`
	Networks    []string  `json:"networks"`
	EnvFiles    []string  `json:"envFiles"`
	Fingerprint string    `json:"fingerprint"`
}

// Variables lists Compose interpolation names without sending their values back
// to the browser. Compose itself remains authoritative during validation.
func Variables(content string) []Variable {
	values := map[string]Variable{}
	for _, match := range interpolationPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[0] > 0 && content[match[0]-1] == '$' {
			continue
		}
		name := content[match[2]:match[3]]
		modifier := content[match[4]:match[5]]
		variable := Variable{Name: name, Required: !strings.HasPrefix(modifier, "-") && !strings.HasPrefix(modifier, ":-") && !strings.HasPrefix(modifier, "+") && !strings.HasPrefix(modifier, ":+")}
		if strings.HasPrefix(modifier, ":-") {
			variable.Default = strings.TrimPrefix(modifier, ":-")
		} else if strings.HasPrefix(modifier, "-") {
			variable.Default = strings.TrimPrefix(modifier, "-")
		}
		if previous, ok := values[name]; ok && previous.Required {
			variable.Required = true
		}
		values[name] = variable
	}
	result := make([]Variable, 0, len(values))
	for _, variable := range values {
		result = append(result, variable)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func Validate(ctx context.Context, name string, source Source, variables map[string]string, envFiles map[string]string) (Preview, error) {
	if !projectNamePattern.MatchString(name) {
		return Preview{}, fmt.Errorf("%w: project name must use lowercase letters, numbers, hyphens, or underscores", ErrInvalidSource)
	}
	if err := validateSourceStructure(source.YAML); err != nil {
		return Preview{}, err
	}
	for _, variable := range Variables(source.YAML) {
		if variable.Required {
			_, supplied := variables[variable.Name]
			if !supplied && !envFileDefines(envFiles[".env"], variable.Name) {
				return Preview{}, fmt.Errorf("%w: provide %s before validation", ErrInvalidSource, variable.Name)
			}
		}
	}
	for key := range variables {
		if !variableNamePattern.MatchString(key) || strings.HasPrefix(key, "COMPOSE_") || strings.HasPrefix(key, "DOCKER_") || key == "PATH" || key == "HOME" || key == "USER" || key == "TMPDIR" {
			return Preview{}, fmt.Errorf("%w: invalid interpolation variable name", ErrInvalidSource)
		}
	}
	composePath, cleanup, err := prepareCompose(source.YAML, envFiles)
	if err != nil {
		return Preview{}, err
	}
	defer cleanup()
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, "docker", "compose", "--ansi", "never", "-p", name, "-f", composePath, "config", "--format", "json", "--no-path-resolution")
	command.Dir = filepath.Dir(composePath)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for key, value := range variables {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if len(envFiles) > 0 || len(variables) > 0 {
				return Preview{}, fmt.Errorf("%w: Compose validation failed; check the YAML and environment file syntax", ErrInvalidSource)
			}
			message := strings.TrimSpace(string(exit.Stderr))
			if len(message) > 500 {
				message = message[:500]
			}
			return Preview{}, fmt.Errorf("%w: Compose validation failed: %s", ErrInvalidSource, message)
		}
		return Preview{}, fmt.Errorf("Compose validation is unavailable: %w", err)
	}
	var model struct {
		Services map[string]struct {
			Image string `json:"image"`
			Ports []struct {
				Published string `json:"published"`
				Target    int    `json:"target"`
				Protocol  string `json:"protocol"`
			} `json:"ports"`
			Volumes []struct {
				Type   string `json:"type"`
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"volumes"`
			Networks map[string]any `json:"networks"`
		} `json:"services"`
		Volumes  map[string]any `json:"volumes"`
		Networks map[string]any `json:"networks"`
	}
	if err := json.Unmarshal(output, &model); err != nil {
		return Preview{}, fmt.Errorf("cannot read Compose validation result: %w", err)
	}
	if len(model.Services) == 0 {
		return Preview{}, fmt.Errorf("%w: Compose file has no services", ErrInvalidSource)
	}
	preview := Preview{Name: name, Services: []Service{}, Volumes: []string{}, Networks: []string{}, EnvFiles: []string{}}
	info, _ := InspectSource(source.YAML)
	for _, file := range info.EnvFiles {
		preview.EnvFiles = append(preview.EnvFiles, file.Path)
	}
	for name, service := range model.Services {
		if service.Image == "" {
			return Preview{}, fmt.Errorf("%w: service %s needs a prebuilt image", ErrInvalidSource, name)
		}
		item := Service{Name: name, Image: service.Image, Ports: []string{}, Volumes: []string{}, Networks: []string{}}
		for _, port := range service.Ports {
			item.Ports = append(item.Ports, fmt.Sprintf("%s:%d/%s", port.Published, port.Target, port.Protocol))
		}
		for _, volume := range service.Volumes {
			if volume.Type == "bind" && !strings.HasPrefix(volume.Source, "/") {
				return Preview{}, fmt.Errorf("%w: service %s uses a relative bind mount", ErrInvalidSource, name)
			}
			if volume.Type != "bind" && volume.Type != "volume" {
				return Preview{}, fmt.Errorf("%w: service %s uses an unsupported mount", ErrInvalidSource, name)
			}
			item.Volumes = append(item.Volumes, volume.Source+":"+volume.Target)
		}
		for network := range service.Networks {
			item.Networks = append(item.Networks, network)
		}
		sort.Strings(item.Ports)
		sort.Strings(item.Volumes)
		sort.Strings(item.Networks)
		preview.Services = append(preview.Services, item)
	}
	for name := range model.Volumes {
		preview.Volumes = append(preview.Volumes, name)
	}
	for name := range model.Networks {
		preview.Networks = append(preview.Networks, name)
	}
	sort.Slice(preview.Services, func(i, j int) bool { return preview.Services[i].Name < preview.Services[j].Name })
	sort.Strings(preview.Volumes)
	sort.Strings(preview.Networks)
	canonical, _ := json.Marshal(struct {
		Name      string
		YAML      string
		Variables map[string]string
		EnvFiles  map[string]string
	}{name, source.YAML, variables, envFiles})
	fingerprint := sha256.Sum256(canonical)
	preview.Fingerprint = hex.EncodeToString(fingerprint[:])
	return preview, nil
}

func validateSourceStructure(content string) error {
	var model map[string]any
	if err := yaml.Unmarshal([]byte(content), &model); err != nil {
		return fmt.Errorf("%w: invalid YAML: %v", ErrInvalidSource, err)
	}
	if model == nil || model["services"] == nil {
		return fmt.Errorf("%w: Compose file has no services", ErrInvalidSource)
	}
	for _, key := range []string{"include", "configs", "secrets"} {
		if model[key] != nil {
			return fmt.Errorf("%w: %s requires local dependencies", ErrInvalidSource, key)
		}
	}
	if volumes, ok := model["volumes"].(map[string]any); ok {
		for name, raw := range volumes {
			if definition, ok := raw.(map[string]any); ok && definition["driver_opts"] != nil {
				return fmt.Errorf("%w: volume %s uses unsupported driver options", ErrInvalidSource, name)
			}
		}
	}
	services, ok := model["services"].(map[string]any)
	if !ok {
		return fmt.Errorf("%w: invalid services", ErrInvalidSource)
	}
	for name, raw := range services {
		service, ok := raw.(map[string]any)
		if !ok || service["image"] == nil || service["build"] != nil {
			return fmt.Errorf("%w: service %s must use a prebuilt image", ErrInvalidSource, name)
		}
		for _, key := range []string{"extends", "configs", "secrets", "develop", "label_file", "credential_spec"} {
			if service[key] != nil {
				return fmt.Errorf("%w: service %s uses unsupported %s", ErrInvalidSource, name, key)
			}
		}
	}
	return nil
}
