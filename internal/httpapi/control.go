package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

func (s *Server) controlHandler() http.Handler {
	mux := http.NewServeMux()
	register := func(path, method string, public bool, handler http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !public && !s.requireControlAuth(w, r) {
				return
			}
			if r.Method != method {
				w.Header().Set("Allow", method)
				writeControlError(w, 405, "METHOD_NOT_ALLOWED", "Method is not supported for this route.")
				return
			}
			handler(w, r)
		})
	}
	register("/v1/health", "GET", true, s.controlHealth)
	register("/v1/info", "GET", true, s.controlInfo)
	register("/v1/status", "GET", false, s.controlStatus)
	register("/v1/projects", "GET", false, s.controlProjects)
	register("/v1/containers/{id}", "GET", false, s.controlInspection)
	for _, target := range []string{"containers", "projects"} {
		isContainer := target == "containers"
		register("/v1/"+target+"/{id}/actions", "POST", false, func(w http.ResponseWriter, r *http.Request) { s.controlAction(w, r, isContainer) })
		register("/v1/"+target+"/{id}/pull", "POST", false, func(w http.ResponseWriter, r *http.Request) { s.controlPull(w, r, isContainer) })
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeControlError(w, 404, "ROUTE_NOT_FOUND", "Route does not exist.")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// Reject noncanonical paths rather than letting ServeMux emit HTML redirects.
		for _, segment := range strings.Split(r.URL.Path, "/")[1:] {
			if segment == "" || segment == "." || segment == ".." {
				writeControlError(w, 404, "ROUTE_NOT_FOUND", "Route does not exist.")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) controlHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	result := controlHealth{Service: "nox-yard", Version: s.controlVersion(), APIVersion: "v1", Ready: true, Storage: controlAvailability{Available: true}}
	status := http.StatusOK
	if err := s.store.PingContext(ctx); err != nil {
		status = http.StatusServiceUnavailable
		result.Ready, result.Storage.Available = false, false
		result.Code, result.Message = "STORAGE_UNAVAILABLE", "Storage is unavailable."
	}
	writeJSON(w, status, result)
}

func (s *Server) controlInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, controlInfo{Service: "nox-yard", Version: s.controlVersion(), APIVersion: "v1", APIEnabled: s.control.enabled, Capabilities: []string{"docker-status", "project-inventory", "container-inspection", "container-lifecycle", "project-lifecycle", "container-image-pull", "project-image-pull"}})
}

func (s *Server) controlStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snapshot, err := s.readProjects(ctx)
	result := controlStatus{Service: "nox-yard", Version: s.controlVersion(), APIVersion: "v1"}
	if err != nil {
		var read inventoryReadError
		status, problem := classifyControlError(err, false)
		if !errors.As(err, &read) || (status != 502 && status != 503 && status != 504) || errors.Is(err, context.Canceled) {
			writeJSON(w, status, problem)
			return
		}
		result.Docker.Error = &problem
	} else {
		result.Docker.Available = true
		counts := controlCounts{CollectedAt: snapshot.CollectedAt.UTC(), Projects: len(snapshot.Projects)}
		for _, project := range snapshot.Projects {
			counts.Containers += len(project.Containers)
			for _, container := range project.Containers {
				if container.State == "running" {
					counts.RunningContainers++
				}
			}
		}
		result.Inventory = &counts
	}
	writeJSON(w, 200, result)
}

func (s *Server) controlProjects(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.readProjects(r.Context())
	if err != nil {
		writeControlCause(w, err)
		return
	}
	writeJSON(w, 200, projectsDTO(snapshot))
}

func validControlTarget(id string, container bool) bool {
	if container {
		return containerIDPattern.MatchString(id)
	}
	if suffix, ok := strings.CutPrefix(id, "container:"); ok {
		return containerIDPattern.MatchString(suffix)
	}
	name, ok := strings.CutPrefix(id, "compose:")
	return ok && name != "" && !strings.ContainsAny(name, "/\\") && !strings.ContainsFunc(name, unicode.IsControl)
}

func checkControlTarget(w http.ResponseWriter, r *http.Request, container bool) bool {
	if validControlTarget(r.PathValue("id"), container) {
		return true
	}
	writeControlError(w, 400, "INVALID_TARGET_ID", "Invalid target ID.")
	return false
}

func (s *Server) controlInspection(w http.ResponseWriter, r *http.Request) {
	if !checkControlTarget(w, r, true) {
		return
	}
	if s.inventory == nil {
		writeControlCause(w, errDockerUnavailable)
		return
	}
	detail, err := s.inventory.InspectContainer(r.Context(), r.PathValue("id"), false)
	if err != nil {
		writeControlCause(w, err)
		return
	}
	if detail.ID != r.PathValue("id") {
		writeControlCause(w, inventory.ErrContainerNotFound)
		return
	}
	writeJSON(w, 200, inspectionDTO(detail))
}

func decodeControlAction(w http.ResponseWriter, r *http.Request) (lifecycle.Action, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeControlError(w, 415, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json.")
		return "", false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeControlError(w, 413, "PAYLOAD_TOO_LARGE", "Payload exceeds 4 KiB.")
		return "", false
	}
	action, valid := parseControlAction(body)
	if err != nil || !valid {
		writeControlError(w, 400, "INVALID_PAYLOAD", "Provide one JSON object with action start, stop, or restart.")
		return "", false
	}
	return action, true
}

// Require the exact request field and reject duplicate keys rather than letting
// encoding/json silently choose the last action in an ambiguous payload.
func parseControlAction(body []byte) (lifecycle.Action, bool) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return "", false
	}
	name, err := decoder.Token()
	if err != nil || name != "action" {
		return "", false
	}
	var action lifecycle.Action
	if decoder.Decode(&action) != nil || decoder.More() {
		return "", false
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !errors.Is(decoder.Decode(new(any)), io.EOF) || (action != lifecycle.Start && action != lifecycle.Stop && action != lifecycle.Restart) {
		return "", false
	}
	return action, true
}

func (s *Server) controlAction(w http.ResponseWriter, r *http.Request, container bool) {
	if !checkControlTarget(w, r, container) {
		return
	}
	action, ok := decodeControlAction(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.runAction(ctx, r.PathValue("id"), container, action)
	dto := controlActionResult{Action: string(action), Succeeded: result.Succeeded, Skipped: result.Skipped, Failed: result.Failed, Queued: result.Queued, Failures: failuresDTO(result.Failures)}
	writeControlResult(w, ctx, err, dto, result.Failed, result.Succeeded, result.Skipped, result.Queued, result.Failures)
}

func (s *Server) controlPull(w http.ResponseWriter, r *http.Request, container bool) {
	if !checkControlTarget(w, r, container) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 {
		writeControlError(w, 400, "INVALID_PAYLOAD", "Pull requires an empty body.")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	result, err := s.runPull(ctx, r.PathValue("id"), container)
	dto := controlPullResult{Succeeded: result.Succeeded, Failed: result.Failed, Images: append([]string{}, result.Images...), Failures: failuresDTO(result.Failures)}
	writeControlResult(w, ctx, err, dto, result.Failed, result.Succeeded, 0, 0, result.Failures)
}
