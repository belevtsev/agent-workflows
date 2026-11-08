// Package client is the Go SDK for the jobs API. Customers vendor a tagged
// release of it, so a running integration may be several releases behind the
// server.
package client

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Status mirrors the server's job status response.
type Status struct {
	ID             string `json:"id"`
	State          string `json:"state"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Get fetches a job's status.
func (c *Client) Get(id string) (Status, error) {
	resp, err := c.http.Get(c.base + "/v1/jobs/" + id)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("jobs: status %d", resp.StatusCode)
	}
	var s Status
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return Status{}, err
	}
	if s.TimeoutSeconds == 0 {
		return Status{}, fmt.Errorf("jobs: response has no timeout_seconds")
	}
	return s, nil
}
