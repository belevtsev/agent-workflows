// Package sandboxtest provisions artifact-only developer acceptance runs.
// Behavioral assertions belong to the independently authored Probe.
package sandboxtest

type Options struct {
	Source  string
	Backend string
	Output  string
}

type Artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

type Resource struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

type Report struct {
	Backend        string         `json:"backend"`
	Status         string         `json:"status"`
	Available      bool           `json:"available"`
	SourceRevision string         `json:"source_revision"`
	OS             string         `json:"os"`
	Arch           string         `json:"arch"`
	Output         string         `json:"output"`
	Artifacts      []Artifact     `json:"artifacts"`
	Resources      []Resource     `json:"resources"`
	Probe          map[string]any `json:"probe,omitempty"`
	DurationMS     int64          `json:"duration_ms"`
	Limitations    []string       `json:"limitations"`
	CleanupErrors  []string       `json:"cleanup_errors"`
}
