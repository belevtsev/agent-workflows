package repocheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// An SVG can faithfully reproduce a diagram that lets work skip review or proof.
// Check the routes a developer can actually follow in the published drawings.
func TestDocumentedWorkflowPaths(t *testing.T) {
	for _, tc := range []struct {
		diagram, start, end string
		required, optional  []string
	}{
		{"daily-task", "ask", "result", []string{"skill", "saved", "next"}, nil},
		{"daily-task", "resume", "result", []string{"next"}, []string{"work"}},
		{"task-workflow", "lookup", "commit", []string{"spec-review", "spec-approval", "design-review", "design-approval", "code-review"}, nil},
		{"task-workflow", "small", "commit", []string{"tests", "checks", "code-review"}, []string{"code"}},
		{"ticket-delivery", "ticket", "receipt", []string{"snapshot", "failure", "repair", "retry", "commit", "on-commit"}, nil},
		{"ticket-delivery", "on-commit", "log", nil, []string{"receipt"}},
	} {
		t.Run(tc.diagram+"/"+tc.start+"-to-"+tc.end, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(repoRoot, diagramFolder, tc.diagram+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var spec diagramSpec
			if err := json.Unmarshal(data, &spec); err != nil {
				t.Fatal(err)
			}
			if !diagramPathWithout(spec.Edges, tc.start, tc.end, "") {
				t.Fatalf("no path from %s to %s", tc.start, tc.end)
			}
			for _, step := range tc.required {
				if diagramPathWithout(spec.Edges, tc.start, tc.end, step) {
					t.Errorf("%s can reach %s without %s", tc.start, tc.end, step)
				}
			}
			for _, step := range tc.optional {
				if !diagramPathWithout(spec.Edges, tc.start, tc.end, step) {
					t.Errorf("every path from %s to %s requires %s", tc.start, tc.end, step)
				}
			}
		})
	}
}

func diagramPathWithout(edges []diagramArrow, start, end, skip string) bool {
	seen := map[string]bool{skip: true}
	queue := []string{start}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if seen[node] {
			continue
		}
		if node == end {
			return true
		}
		seen[node] = true
		for _, edge := range edges {
			if edge.From == node {
				queue = append(queue, edge.To)
			}
		}
	}
	return false
}
