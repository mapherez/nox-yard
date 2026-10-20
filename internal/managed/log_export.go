package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/logexport"
)

// Every export uses the configured base, including externally located Compose
// projects. Runtime working directories and browser-supplied paths are not used.
func (m *Manager) ProjectLogExport(ctx context.Context, name string, content *string) (logexport.Status, error) {
	if !logexport.ValidProjectName(name) {
		return logexport.Status{}, fmt.Errorf("%w: invalid project directory", ErrInvalidSource)
	}
	base, err := m.ProjectsBase()
	if err != nil {
		return logexport.Status{}, err
	}
	base, err = NormalizeProjectsBase(base)
	if err != nil {
		return logexport.Status{}, err
	}
	image, err := helperImage(ctx)
	if err != nil {
		return logexport.Status{}, fmt.Errorf("%w: host log exports require the Yard container runtime", ErrConflict)
	}
	mode, mount := "read", "type=bind,source="+base+",target=/export-root"
	if content == nil {
		mount += ",readonly"
	} else {
		mode = "write"
	}
	args := helperCommandArgs("-i", "--network", "none", "--read-only", "--mount", mount, "--entrypoint", "nox-yard", image, "log-export-helper", mode, name, "create", time.Now().Format("2006-01-02-15-04"))
	command := exec.CommandContext(ctx, "docker", args...)
	if content != nil {
		command.Stdin = strings.NewReader(*content)
	}
	output, err := command.Output()
	if err != nil {
		return logexport.Status{}, fmt.Errorf("%w: cannot access the configured project logs directory; check host permissions and symbolic links", ErrConflict)
	}
	var result logexport.Status
	if json.Unmarshal(output, &result) != nil {
		return logexport.Status{}, fmt.Errorf("%w: invalid log export response", ErrConflict)
	}
	result.Directory = path.Join(base, name, "logs")
	if stamp, _, valid := logexport.FileTime(result.Filename, time.Local); valid {
		result.ExportedAt = stamp.Format(time.RFC3339)
	}
	return result, nil
}
