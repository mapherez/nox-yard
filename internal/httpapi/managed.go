package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/mapherez/nox-yard/internal/managed"
)

func decodeManaged(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, managed.MaxSourceBytes*2)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid managed project request.")
		return false
	}
	return true
}

func (s *Server) managedPreview(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "Managed projects are unavailable.")
		return
	}
	var input managed.Request
	if !decodeManaged(w, r, &input) {
		return
	}
	preview, _, err := s.managed.Preview(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) managedDeploy(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "Managed projects are unavailable.")
		return
	}
	var input managed.Request
	if !decodeManaged(w, r, &input) {
		return
	}
	job, err := s.managed.Submit(r.Context(), input)
	if errors.Is(err, managed.ErrConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) managedJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "Managed projects are unavailable.")
		return
	}
	job, found, err := s.managed.Job(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read operation status.")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "Operation not found.")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) managedOperation(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "Managed projects are unavailable.")
		return
	}
	var input struct {
		Operation     string `json:"operation"`
		RemoveVolumes bool   `json:"removeVolumes"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	job, err := s.managed.Operation(r.PathValue("name"), input.Operation, input.RemoveVolumes)
	if errors.Is(err, managed.ErrConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
