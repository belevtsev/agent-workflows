package contextpack

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

type cursor struct {
	Artifact string `json:"a"`
	Item     string `json:"i"`
	Offset   int    `json:"o"`
}

func nextCursor(id, item string, offset int) string {
	data, _ := json.Marshal(cursor{id, item, offset})
	return base64.RawURLEncoding.EncodeToString(data)
}

// Render returns exact-budget JSON, including its trailing newline. All item outcomes and
// metadata survive even when no lines fit. Every omitted line has an expansion cursor.
func Render(a Artifact, itemID, continuation string, budget int, hit bool) ([]byte, error) {
	if budget < 512 || budget > MaxBytes {
		return nil, fmt.Errorf("--max-bytes must be between 512 and %d", MaxBytes)
	}
	offset := 0
	if continuation != "" {
		var err error
		offset, err = parseCursor(a.ID, itemID, continuation)
		if err != nil {
			return nil, err
		}
	}
	p, originals, err := makePage(a, itemID, offset, hit)
	if err != nil {
		return nil, err
	}
	encode := func() []byte { data, _ := json.Marshal(p); return append(data, '\n') }
	data := encode()
	if len(data) > budget {
		return nil, fmt.Errorf("budget cannot hold required metadata and outcomes (%d bytes); increase --max-bytes", len(data))
	}
	// Round-robin allocation gives each operation evidence before spending the budget on one.
	for progress := true; progress; {
		progress = false
		for i, original := range originals {
			page := &p.Items[i]
			position := offset + len(page.Lines)
			if position >= len(original.Lines) {
				continue
			}
			previousNext := page.Next
			line := original.Lines[position]
			if page.Path != "" {
				line.Path = ""
			}
			page.Lines = append(page.Lines, line)
			page.Omitted--
			page.Next = ""
			if page.Omitted > 0 {
				next := position + 1
				if page.Preview {
					next = 0
				}
				page.Next = nextCursor(a.ID, page.ID, next)
			}
			candidate := encode()
			if len(candidate) > budget {
				page.Lines = page.Lines[:len(page.Lines)-1]
				page.Omitted++
				page.Next = previousNext
				continue
			}
			data, progress = candidate, true
		}
	}
	if itemID != "" && len(p.Items[0].Lines) == 0 && p.Items[0].Omitted > 0 {
		return nil, errors.New("budget holds metadata but no evidence; increase --max-bytes")
	}
	return data, nil
}

func makePage(a Artifact, itemID string, offset int, hit bool) (Page, []Item, error) {
	p := Page{Version: 1, ID: a.ID, Kind: a.Kind, Created: a.Created, State: a.State, Metadata: a.Metadata, CacheHit: hit, Items: []PageItem{}}
	var originals []Item
	for _, item := range a.Items {
		if itemID != "" && item.ID != itemID {
			continue
		}
		if offset > len(item.Lines) {
			return p, nil, errors.New("cursor is beyond this item's evidence")
		}
		originals = append(originals, item)
		stub := item
		stub.Lines = nil
		stub.Path = commonPath(item.Lines)
		total := max(len(item.Lines), item.LogChunks)
		page := PageItem{Item: stub, Total: total, Omitted: total - offset}
		if page.Omitted > 0 {
			page.Next = nextCursor(a.ID, item.ID, offset)
		}
		p.Items = append(p.Items, page)
		if a.Kind == "checks" && itemID == "" {
			p.Items[len(p.Items)-1].Preview = true
			originals[len(originals)-1].Lines = checkPreview(item)
		}
	}
	if itemID != "" && len(p.Items) == 0 {
		return p, nil, errors.New("unknown artifact item")
	}
	return p, originals, nil
}

func commonPath(lines []Line) string {
	if len(lines) == 0 {
		return ""
	}
	name := lines[0].Path
	for _, line := range lines[1:] {
		if line.Path != name {
			return ""
		}
	}
	return name
}

func parseCursor(id, item, continuation string) (int, error) {
	data, err := base64.RawURLEncoding.DecodeString(continuation)
	var c cursor
	if err != nil || json.Unmarshal(data, &c) != nil || item == "" || c.Artifact != id || c.Item != item || c.Offset < 0 {
		return 0, errors.New("cursor must match this artifact and --item")
	}
	return c.Offset, nil
}

func checkPreview(item Item) []Line {
	if item.Status == "passed" || item.Status == "satisfied" {
		return nil
	}
	if len(item.Lines) <= 4 {
		return item.Lines
	}
	return append(append([]Line{}, item.Lines[len(item.Lines)-3:]...), item.Lines[0])
}
