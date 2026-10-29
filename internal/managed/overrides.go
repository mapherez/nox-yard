package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/store"
	"gopkg.in/yaml.v3"
)

var ErrConfigurationRecovery = errors.New("configuration edit requires recovery review")

type OverrideRequest struct {
	Variables            map[string]string `json:"variables,omitempty"`
	Files                map[string]string `json:"files,omitempty"`
	Action               string            `json:"action"`
	Path                 string            `json:"path,omitempty"`
	Source               SourceInput       `json:"source"`
	Fingerprint          string            `json:"fingerprint"`
	CandidateFingerprint string            `json:"candidateFingerprint,omitempty"`
}
type OverridePreview struct {
	Variables            map[string]string `json:"variables,omitempty"`
	Files                map[string]string `json:"files,omitempty"`
	Action               string            `json:"action"`
	Path                 string            `json:"path"`
	Source               SourceInput       `json:"source"`
	Fingerprint          string            `json:"fingerprint"`
	CandidateFingerprint string            `json:"candidateFingerprint"`
	Before               string            `json:"before"`
	After                string            `json:"after"`
	ComposeFiles         []string          `json:"composeFiles"`
}
type configurationJournal struct {
	Before FileSnapshot
	After  FileSnapshot
}

func effectiveYAML(s FileSnapshot) (string, error) {
	definition, err := resolvedDefinition(filePreview(s), s.Directory, nil)
	if err != nil {
		return "", err
	}
	var value any
	if err = json.Unmarshal([]byte(definition), &value); err != nil {
		return "", err
	}
	content, err := yaml.Marshal(value)
	return string(content), err
}

func (m *Manager) prepareOverride(ctx context.Context, name string, input OverrideRequest) (OverridePreview, FileSnapshot, FileSnapshot, error) {
	before, err := m.readFileSnapshotMode(ctx, name, input.Action == "remove")
	if err != nil {
		return OverridePreview{}, before, before, err
	}
	if before.Fingerprint != input.Fingerprint {
		return OverridePreview{}, before, before, fmt.Errorf("%w: configuration changed; reload before reviewing the override", ErrConflict)
	}
	after := before
	after.Files = map[string]string{}
	after.ComposeFiles = slices.Clone(before.ComposeFiles)
	for file, content := range before.Files {
		after.Files[file] = content
	}
	view := OverridePreview{Action: input.Action, Fingerprint: before.Fingerprint, Variables: input.Variables, Files: input.Files}
	newWrites := map[string]bool{}
	for key := range input.Variables {
		if !variableNamePattern.MatchString(key) || strings.HasPrefix(key, "COMPOSE_") || strings.HasPrefix(key, "DOCKER_") || slices.Contains([]string{"PATH", "HOME", "USER", "TMPDIR"}, key) {
			return view, before, after, ErrInvalidSource
		}
	}
	for name, content := range input.Files {
		if !safeProjectFile(name) || slices.Contains(before.ComposeFiles, name) || !utf8.ValidString(content) || len(content) > MaxSourceBytes {
			return view, before, after, ErrInvalidSource
		}
		current, err := readConfigurationFiles(ctx, before.Directory, []string{name})
		if err != nil {
			return view, before, after, err
		}
		if previous, exists := current[name]; exists {
			if previous != content {
				return view, before, after, fmt.Errorf("%w: project file %s already exists; edit it through its file editor", ErrConflict, name)
			}
			before.Files[name] = previous
		} else {
			newWrites[name] = true
		}
		after.Files[name] = content
	}
	if len(input.Variables) > 0 {
		if _, exists := before.Files[".env"]; !exists {
			newWrites[".env"] = true
		}
		after.Files[".env"] = materializedEnvironment(input.Variables, after.Files)[".env"]
	}
	switch input.Action {
	case "add":
		loader := m.loadSource
		if loader == nil {
			loader = LoadSource
		}
		source, err := loader(ctx, input.Source)
		if err != nil {
			return view, before, after, err
		}
		for n := 1; n <= 128; n++ {
			name := "compose.override.yaml"
			if n > 1 {
				name = fmt.Sprintf("compose.override.%d.yaml", n)
			}
			if slices.Contains(before.ComposeFiles, name) {
				continue
			}
			existing, err := readConfigurationFiles(ctx, before.Directory, []string{name})
			if err != nil {
				return view, before, after, err
			}
			if _, exists := existing[name]; exists {
				continue
			}
			view.Path = name
			break
		}
		if view.Path == "" {
			return view, before, after, fmt.Errorf("%w: too many override files", ErrInvalidSource)
		}
		if input.Path != "" && input.Path != view.Path {
			return view, before, after, fmt.Errorf("%w: override destination changed; review again", ErrConflict)
		}
		view.Source = SourceInput{Kind: "paste", YAML: source.YAML}
		newWrites[view.Path] = true
		after.Files[view.Path] = source.YAML
		after.ComposeFiles = append(after.ComposeFiles, view.Path)
	case "remove":
		if len(before.ComposeFiles) < 2 || input.Path == before.ComposeFiles[0] || !slices.Contains(before.ComposeFiles, input.Path) {
			return view, before, after, ErrInvalidSource
		}
		view.Path = input.Path
		after.ComposeFiles = slices.DeleteFunc(after.ComposeFiles, func(name string) bool { return name == input.Path })
	default:
		return view, before, after, ErrInvalidSource
	}
	if err := snapshotDependencies(ctx, &after, true); err != nil {
		return view, before, after, err
	}
	// Newly referenced dependencies already exist on disk; never treat them as
	// new writes or remove them during recovery.
	for name, content := range after.Files {
		if !newWrites[name] {
			if _, ok := before.Files[name]; !ok {
				before.Files[name] = content
			}
		}
	}
	if err := resolveFileSnapshot(ctx, &after); err != nil {
		return view, before, after, err
	}
	after.Context.Revision++
	view.Before, err = effectiveYAML(before)
	if err != nil {
		if input.Action != "remove" {
			return view, before, after, err
		}
		view.Before = "# The previous selection could not be resolved; review the complete resulting configuration.\n"
	}
	view.After, err = effectiveYAML(after)
	if err != nil {
		return view, before, after, err
	}
	view.ComposeFiles = slices.Clone(after.ComposeFiles)
	encoded, _ := json.Marshal(struct{ Base, Action, Path, Source, After string }{input.Fingerprint, input.Action, view.Path, view.Source.YAML, fileFingerprint(after)})
	view.CandidateFingerprint = fingerprint(encoded)
	return view, before, after, nil
}

func (m *Manager) PreviewOverride(ctx context.Context, name string, input OverrideRequest) (OverridePreview, error) {
	view, _, _, err := m.prepareOverride(ctx, name, input)
	return view, err
}

func (m *Manager) ApplyOverride(ctx context.Context, name string, input OverrideRequest) (FileConfigurationView, error) {
	return m.configurationJob(ctx, name, func(ctx context.Context) (FileConfigurationView, error) { return m.applyOverride(ctx, name, input) })
}
func (m *Manager) applyOverride(ctx context.Context, name string, input OverrideRequest) (FileConfigurationView, error) {
	// Apply accepts only reviewed bytes, never a URL to fetch a second time.
	if input.Action == "add" && input.Source.Kind != "paste" {
		return FileConfigurationView{}, ErrInvalidSource
	}
	view, before, after, err := m.prepareOverride(ctx, name, input)
	if err != nil {
		return FileConfigurationView{}, err
	}
	if input.CandidateFingerprint == "" || input.CandidateFingerprint != view.CandidateFingerprint {
		return FileConfigurationView{}, fmt.Errorf("%w: override preview changed; review again", ErrConflict)
	}
	if err := m.commitConfigurationEdit(ctx, before, after); err != nil {
		return FileConfigurationView{}, err
	}
	if m.changes != nil {
		m.changes.Notify(inventory.Change{Inventory: true})
	}
	return m.Configuration(ctx, name)
}

func (m *Manager) configurationJob(ctx context.Context, name string, operation func(context.Context) (FileConfigurationView, error)) (FileConfigurationView, error) {
	if owned, ok := jobs.FromContext(ctx); ok {
		if owned.TargetID != "compose:"+name {
			return FileConfigurationView{}, store.ErrJobChanged
		}
		return operation(ctx)
	}
	job, err := store.NewJob("compose:"+name, "engine", "edit_configuration", []string{"compose:" + name})
	if err != nil {
		return FileConfigurationView{}, err
	}
	if err = m.data.CreateJob(job); err != nil {
		return FileConfigurationView{}, err
	}
	view, err := operation(jobs.WithJob(ctx, job))
	status, outcome, message := "succeeded", "verified", ""
	if err != nil {
		status, outcome, message = "failed", "failed", "Configuration could not be saved."
	}
	if errors.Is(err, ErrConfigurationRecovery) || ctx.Err() != nil {
		status, outcome, message = "failed", "recovery_required", "Configuration completion is uncertain; review the private file journal before retrying."
	}
	if finishErr := m.data.FinishJob(job.ID, job.Owner, status, outcome, message, ""); finishErr != nil {
		return FileConfigurationView{}, errors.Join(err, finishErr)
	}
	return view, err
}
func (m *Manager) commitConfigurationEdit(ctx context.Context, before, after FileSnapshot) error {
	job, ok := jobs.FromContext(ctx)
	if !ok {
		return ErrConflict
	}
	payload := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(job.Payload), &payload)
	journal, _ := json.Marshal(configurationJournal{Before: before, After: after})
	payload["configurationEdit"] = journal
	encoded, _ := json.Marshal(payload)
	if err := m.data.JobPayload(job.ID, job.Owner, string(encoded)); err != nil {
		return err
	}
	if err := m.data.JobProgress(job.ID, job.Owner, "saving_configuration", nil, nil); err != nil {
		return err
	}
	if err := saveConfigurationFiles(ctx, before, after); err != nil {
		return err
	}
	if err := m.data.SetFileConfiguration(configurationLocation(after)); err != nil {
		return errors.Join(ErrConfigurationRecovery, err)
	}
	return nil
}
