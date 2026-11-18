package contextpack

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	metadata, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode context page metadata: %w", err)
	}
	initialSize := len(metadata) + 1 // wire format ends in a newline
	checks := (a.Kind == "checks" || a.Kind == "verification") && itemID == ""
	if checks {
		if data := completeCheckPage(p, originals, budget); data != nil {
			return data, nil
		}
	}
	if initialSize > budget {
		return nil, fmt.Errorf("budget cannot hold required metadata and outcomes (%d bytes); increase --max-bytes", initialSize)
	}
	if checks {
		for i := range originals {
			originals[i].Lines = checkPreview(originals[i])
		}
	}
	data := fillPage(&p, originals, offset, budget, initialSize)
	if checks {
		data = completeFittingChecks(&p, a.Items, budget)
	}
	if itemID != "" && len(p.Items[0].Lines) == 0 && p.Items[0].Omitted > 0 {
		return nil, errors.New("budget holds metadata but no evidence; increase --max-bytes")
	}
	return data, nil
}

func fillPage(p *Page, originals []Item, offset, budget, size int) []byte {
	type addition struct {
		ready bool
		line  Line
		next  string
		bytes int
	}
	pending := make([]addition, len(originals))
	// Keep round-robin order. A previously oversized addition may fit once
	// another item's final cursor disappears. Measure each proposed line once.
	for progress := true; progress; {
		progress = false
		for i, original := range originals {
			page := &p.Items[i]
			position := offset + len(page.Lines)
			if position >= len(original.Lines) {
				continue
			}
			add := &pending[i]
			if !add.ready {
				line := original.Lines[position]
				if page.Path != "" {
					line.Path = ""
				}
				next := ""
				if page.Omitted > 1 {
					n := position + 1
					if page.Preview {
						n = 0
					}
					next = nextCursor(p.ID, page.ID, n)
				}
				encoded, _ := json.Marshal(line)
				extra := 1 // comma between lines already in the array
				if len(page.Lines) == 0 {
					extra = len(`,"lines":[]`)
				}
				delta := len(encoded) + extra + positionSize(page.Omitted-1, next) - positionSize(page.Omitted, page.Next)
				*add = addition{true, line, next, delta}
			}
			if size+add.bytes > budget {
				continue
			}
			size += add.bytes
			page.Lines = append(page.Lines, add.line)
			page.Omitted--
			page.Next = add.next
			add.ready = false
			progress = true
		}
	}
	data, _ := json.Marshal(p)
	return append(data, '\n')
}

// Only the omission count and optional cursor change outside the lines array.
// nextCursor produces raw URL-base64, which needs no JSON escaping.
func positionSize(omitted int, next string) int {
	size := len(strconv.Itoa(omitted))
	if next != "" {
		size += len(`,"next_cursor":""`) + len(next)
	}
	return size
}

func completeCheckPage(p Page, originals []Item, budget int) []byte {
	p.Items = append([]PageItem{}, p.Items...)
	for i, original := range originals {
		if original.Status == "passed" || original.Status == "satisfied" {
			continue
		}
		if len(original.Lines) != p.Items[i].Total {
			return nil
		}
		p.Items[i] = completeCheckItem(p.Items[i], original)
	}
	data, _ := json.Marshal(p)
	if len(data)+1 > budget {
		return nil
	}
	return append(data, '\n')
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
		if (a.Kind == "checks" || a.Kind == "verification") && itemID == "" {
			p.Items[len(p.Items)-1].Preview = true
		}
	}
	if itemID != "" && len(p.Items) == 0 {
		return p, nil, errors.New("unknown artifact item")
	}
	return p, originals, nil
}

func completeCheckItem(page PageItem, original Item) PageItem {
	page.Lines = append([]Line{}, original.Lines...)
	if page.Path != "" {
		for i := range page.Lines {
			page.Lines[i].Path = ""
		}
	}
	page.Omitted, page.Next, page.Preview = 0, "", false
	return page
}

func completeFittingChecks(p *Page, originals []Item, budget int) []byte {
	for i, original := range originals {
		if original.Status == "passed" || original.Status == "satisfied" || len(original.Lines) != p.Items[i].Total {
			continue
		}
		previous := p.Items[i]
		p.Items[i] = completeCheckItem(previous, original)
		if data, _ := json.Marshal(p); len(data)+1 > budget {
			p.Items[i] = previous
		}
	}
	data, _ := json.Marshal(p)
	return append(data, '\n')
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
