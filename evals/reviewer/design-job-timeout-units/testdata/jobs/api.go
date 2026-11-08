package jobs

import (
	"encoding/json"
	"net/http"
)

// JobRequest is the body of POST /v1/jobs.
type JobRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// JobStatus is the body of GET /v1/jobs/{id} and of the POST response.
type JobStatus struct {
	ID             string `json:"id"`
	State          string `json:"state"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Create handles POST /v1/jobs. A timeout must be between 1 and 3600
// seconds.
func (s *Server) Create(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed_body")
		return
	}
	if req.TimeoutSeconds < 1 || req.TimeoutSeconds > 3600 {
		writeError(w, http.StatusBadRequest, "invalid_timeout")
		return
	}
	job := s.queue.Enqueue(req.Command, req.TimeoutSeconds)
	writeJSON(w, http.StatusCreated, JobStatus{ID: job.ID, State: "queued", TimeoutSeconds: req.TimeoutSeconds})
}
