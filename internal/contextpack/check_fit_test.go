package contextpack

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func fittingCheckArtifact(chunks int) Artifact {
	lines := Lines("retained.log", 1, strings.Repeat("x", chunks*1024))
	return Artifact{
		Version: 1, ID: strings.Repeat("f", 64), Kind: "checks",
		Metadata: json.RawMessage(`{"checks":[{"id":"failed","state":"failed"}],"verified":false}`),
		Items:    []Item{{ID: "failed", Status: "failed", Lines: lines, LogChunks: len(lines)}},
	}
}

func decodeCheckPage(t *testing.T, data []byte) Page {
	t.Helper()
	var page Page
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestCompleteFailedChecksFitExactSerializedBudget(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1, 4, 5, 6} {
		t.Run(fmt.Sprintf("%d_chunks", count), func(t *testing.T) {
			a := fittingCheckArtifact(count)
			full, err := Render(a, "", "", DefaultBytes, false)
			if err != nil {
				t.Fatal(err)
			}
			page := decodeCheckPage(t, full)
			item := page.Items[0]
			if item.Preview || item.Omitted != 0 || item.Next != "" || item.Total != count || len(item.Lines) != count {
				t.Fatal("fitting failure was incomplete", item)
			}
			for i, line := range item.Lines {
				if line.Text != a.Items[0].Lines[i].Text || line.Offset != a.Items[0].Lines[i].Offset {
					t.Fatal("complete evidence changed order or byte anchors")
				}
			}
			exact, err := Render(a, "", "", len(full), false)
			if err != nil || string(exact) != string(full) {
				t.Fatalf("exact serialized budget rejected: %v", err)
			}
			short, err := Render(a, "", "", len(full)-1, false)
			if err != nil {
				t.Fatal(err)
			}
			partial := decodeCheckPage(t, short).Items[0]
			if len(short) >= len(full) || partial.Omitted == 0 || partial.Next == "" || !partial.Preview {
				t.Fatal("undersized response claimed complete evidence", partial)
			}
		})
	}
}

func TestCompleteCheckPageKeepsUnknownErrorsAndQuietSuccess(t *testing.T) {
	t.Parallel()
	a := fittingCheckArtifact(1)
	a.Kind = "verification"
	a.Items = append(a.Items,
		Item{ID: "missing", Status: "partial", Error: "retained log unavailable"},
		Item{ID: "later", Status: "not_attempted"},
		Item{ID: "quiet", Status: "passed", Lines: a.Items[0].Lines, LogChunks: len(a.Items[0].Lines)},
	)
	data, err := Render(a, "", "", DefaultBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	page := decodeCheckPage(t, data)
	if page.Items[0].Omitted != 0 || page.Items[0].Preview || page.Items[1].Error == "" || page.Items[1].Status != "partial" || page.Items[2].Status != "not_attempted" {
		t.Fatal("complete presentation changed evidence authority", page.Items)
	}
	quiet := page.Items[3]
	if len(quiet.Lines) != 0 || quiet.Omitted != quiet.Total || quiet.Next == "" {
		t.Fatal("successful log was no longer quiet and expandable", quiet)
	}
}

func TestFailedCheckFitUsesEscapedJSONAndSharedBudget(t *testing.T) {
	t.Parallel()
	a := fittingCheckArtifact(1)
	a.Items[0].Lines = Lines("retained.log", 1, strings.Repeat(`"`, 10000))
	a.Items[0].LogChunks = len(a.Items[0].Lines)
	data, err := Render(a, "", "", DefaultBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	item := decodeCheckPage(t, data).Items[0]
	if !item.Preview || item.Omitted == 0 || item.Next == "" || len(data) > DefaultBytes {
		t.Fatal("raw bytes bypassed JSON escaping budget", item)
	}
	a = fittingCheckArtifact(6)
	second := a.Items[0]
	second.ID = "second"
	a.Items = append(a.Items, second)
	full, err := Render(a, "", "", DefaultBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range decodeCheckPage(t, full).Items {
		if item.Omitted != 0 || item.Preview {
			t.Fatal("combined fitting evidence unnecessarily previewed", item)
		}
	}
	shared, err := Render(a, "", "", len(full)-1, false)
	if err != nil {
		t.Fatal(err)
	}
	page := decodeCheckPage(t, shared)
	if len(shared) >= len(full) || page.Items[0].Omitted+page.Items[1].Omitted == 0 {
		t.Fatal("checks received separate response budgets")
	}
	a.Metadata = json.RawMessage(`{"required":"` + strings.Repeat("x", DefaultBytes) + `"}`)
	if _, err := Render(a, "", "", DefaultBytes, false); err == nil {
		t.Fatal("required metadata was dropped to make evidence fit")
	}
}
