package contextpack

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Compare boundary decisions with full JSON encoding after each tentative line.
// This intentionally simple reference also covers the omitted final cursor.
func TestPagingMatchesFullEncodingAtByteBoundaries(t *testing.T) {
	a := Artifact{Version: 1, ID: "fixture", Kind: "collect"}
	for _, id := range []string{"small", "read", "multi"} {
		item := Item{ID: id, Status: "ok"}
		for i := range 15 {
			line := Line{Number: i + 1, Path: "sample", Text: "é <&> \"x\"\n"}
			if id == "multi" && i%2 == 1 {
				line.Path = "other"
			}
			item.Lines = append(item.Lines, line)
		}
		a.Items = append(a.Items, item)
	}
	for _, offset := range []int{0, 7, 14} {
		item := ""
		if offset > 0 {
			item = "multi"
		}
		for budget := 512; budget < 3600; budget += 7 {
			page, originals, err := makePage(a, item, offset, false)
			if err != nil {
				t.Fatal(err)
			}
			start, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			if len(start)+1 > budget {
				continue
			}
			other := page
			other.Items = append([]PageItem(nil), page.Items...)
			want := referenceFill(other, originals, offset, budget)
			got := fillPage(&page, originals, offset, budget, len(start)+1)
			if !bytes.Equal(want, got) {
				t.Fatalf("offset %d budget %d: got %s, want %s", offset, budget, got, want)
			}
		}
	}
}

func referenceFill(page Page, originals []Item, offset, budget int) []byte {
	for {
		changed := false
		for i, item := range originals {
			p := &page.Items[i]
			n := offset + len(p.Lines)
			if n == len(item.Lines) {
				continue
			}
			prior := *p
			line := item.Lines[n]
			if p.Path != "" {
				line.Path = ""
			}
			p.Lines = append(p.Lines, line)
			p.Omitted--
			p.Next = ""
			if p.Omitted > 0 {
				p.Next = nextCursor(page.ID, p.ID, n+1)
			}
			b, _ := json.Marshal(page)
			if len(b)+1 <= budget {
				changed = true
			} else {
				*p = prior
			}
		}
		if !changed {
			b, _ := json.Marshal(page)
			return append(b, '\n')
		}
	}
}

func BenchmarkRenderLargeRead(b *testing.B) {
	item := Item{ID: "lines", Status: "ok"}
	for i := range 2048 {
		item.Lines = append(item.Lines, Line{Number: i + 1, Text: "worker output <line>\n"})
	}
	a := Artifact{Version: 1, ID: "fixture", Kind: "collect", Items: []Item{item}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Render(a, "", "", MaxBytes, false); err != nil {
			b.Fatal(err)
		}
	}
}

func TestInvalidPageMetadataDoesNotProduceSuccessfulEmptyResponse(t *testing.T) {
	for _, kind := range []string{"collect", "checks", "verification"} {
		a := Artifact{Kind: kind, Metadata: json.RawMessage("{")}
		if data, err := Render(a, "", "", DefaultBytes, false); err == nil || len(data) != 0 {
			t.Fatalf("%s: invalid metadata returned %q, %v", kind, data, err)
		}
	}
}
