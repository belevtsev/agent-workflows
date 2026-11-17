package contextpack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// Decode refuses unknown fields, trailing input and oversized requests.
func Decode(r io.Reader) (Request, error) {
	var request Request
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil || len(data) > MaxFileBytes {
		return request, errors.New("context request exceeds 1 MiB or could not be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&request); err != nil {
		return request, err
	}
	if d.Decode(new(any)) != io.EOF {
		return request, errors.New("context request must contain one JSON object")
	}
	return request, validate(request)
}

func validate(request Request) error {
	if request.Version != 1 || len(request.Operations) == 0 || len(request.Operations) > 32 {
		return errors.New("context requires version 1 and 1 to 32 operations")
	}
	ids := map[string]bool{}
	for _, op := range request.Operations {
		if !validID(op.ID) || ids[op.ID] {
			return errors.New("operation ids must be unique, 1 to 64 letters, digits, hyphens or underscores")
		}
		ids[op.ID] = true
		if op.Path != "" && (!filepath.IsLocal(op.Path) || strings.ContainsAny(op.Path, "\\:\x00") || path.Clean(op.Path) != op.Path) {
			return fmt.Errorf("%s: path must be a clean repository-relative slash path", op.ID)
		}
		if err := validateOperation(op); err != nil {
			return err
		}
	}
	return nil
}

func validateOperation(op Operation) error {
	switch op.Kind {
	case "read":
		if !validRead(op) {
			return fmt.Errorf("%s: read requires a path and valid optional line range", op.ID)
		}
	case "search":
		if op.Pattern == "" || len(op.Pattern) > 4096 || op.Start != 0 || op.End != 0 {
			return fmt.Errorf("%s: search requires a pattern of at most 4096 bytes", op.ID)
		}
		if op.Regex {
			if _, err := regexp.Compile(op.Pattern); err != nil {
				return fmt.Errorf("%s: %w", op.ID, err)
			}
		}
	case "git":
		if op.Path != "" || op.Pattern != "" || op.Regex || op.Start != 0 || op.End != 0 {
			return fmt.Errorf("%s: git takes no search or read fields", op.ID)
		}
	default:
		return fmt.Errorf("%s: unknown operation %q", op.ID, op.Kind)
	}
	return nil
}

func validRead(op Operation) bool {
	return op.Path != "" && op.Start >= 0 && op.End >= 0 && (op.End == 0 || op.End >= max(1, op.Start)) && op.Pattern == "" && !op.Regex
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// Snapshot uses the existing Git snapshot implementation, without installing the gate.
func (e *Engine) Snapshot(ctx context.Context) (State, error) {
	tree, err := e.repo.Snapshot(ctx, filepath.Join(e.dir, "tmp"))
	if err != nil {
		return State{}, err
	}
	head, _, err := e.repo.CommitOf(ctx, "HEAD")
	if err != nil {
		return State{}, err
	}
	ignored, err := e.repo.ContextIgnored(ctx)
	if err != nil {
		return State{}, err
	}
	raw, err := e.rawContent(ctx, tree, ignored)
	return State{tree, head, digest(ignored), raw}, err
}

// Git clean filters and line-ending conversion can hide raw byte changes from tree IDs.
func (e *Engine) rawContent(ctx context.Context, tree string, ignored map[string]bool) (string, error) {
	root, err := os.OpenRoot(e.repo.Root)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	h := sha256.New()
	var readErr error
	err = e.repo.Entries(ctx, tree, func(raw []byte, name string) {
		if readErr != nil || ignored[name] {
			return
		}
		if ctx.Err() != nil {
			readErr = ctx.Err()
			return
		}
		if !bytes.HasPrefix(raw, []byte("100644 ")) && !bytes.HasPrefix(raw, []byte("100755 ")) {
			return
		}
		fmt.Fprintf(h, "%s\x00", name)
		f, err := root.Open(filepath.FromSlash(name))
		if err != nil {
			fmt.Fprintf(h, "error:%v\x00", err)
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			fmt.Fprintf(h, "error:%v\x00", err)
			return
		}
		if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
			fmt.Fprintf(h, "unreadable:%v:%d:%v\x00", info.Mode(), info.Size(), info.ModTime())
			return
		}
		content := sha256.New()
		_, readErr = io.Copy(content, io.LimitReader(f, MaxFileBytes+1))
		fmt.Fprintf(h, "sha256:%x\x00", content.Sum(nil))
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), readErr
}

// Collect executes at most four independent read operations concurrently, retaining request order.
func (e *Engine) Collect(ctx context.Context, request Request, metadata json.RawMessage) (Artifact, bool, error) {
	if err := validate(request); err != nil {
		return Artifact{}, false, err
	}
	state, err := e.Snapshot(ctx)
	if err != nil {
		return Artifact{}, false, err
	}
	key := digest(struct {
		State    State
		Request  Request
		Metadata json.RawMessage
	}{state, request, metadata})
	if a, err := e.Load(ctx, key); err == nil {
		return a, true, nil
	}
	sources := []string{}
	err = e.repo.Entries(ctx, state.Tree, func(raw []byte, name string) {
		if bytes.HasPrefix(raw, []byte("100644 ")) || bytes.HasPrefix(raw, []byte("100755 ")) {
			sources = append(sources, name)
		}
	})
	if err != nil {
		return Artifact{}, false, err
	}
	ignored, err := e.repo.ContextIgnored(ctx)
	if err != nil {
		return Artifact{}, false, err
	}
	sources = filterSources(sources, ignored)
	root, err := os.OpenRoot(e.repo.Root)
	if err != nil {
		return Artifact{}, false, err
	}
	defer func() { _ = root.Close() }()
	items := make([]Item, len(request.Operations))
	var wg sync.WaitGroup
	for worker := range min(4, len(items)) {
		wg.Go(func() {
			for i := worker; i < len(items); i += 4 {
				items[i] = e.collectOne(ctx, root, state, sources, request.Operations[i])
			}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Artifact{}, false, err
	}
	after, err := e.Snapshot(ctx)
	if err != nil || after != state {
		return Artifact{}, false, fmt.Errorf("repository changed during collection or could not be rechecked: %v", err)
	}
	a := Artifact{Version: 1, ID: key, Kind: "collection", State: state, Metadata: metadata, Items: items}
	err = e.Save(ctx, &a)
	return a, false, err
}

func filterSources(sources []string, ignored map[string]bool) []string {
	kept := sources[:0]
	for _, name := range sources {
		if !ignored[name] {
			kept = append(kept, name)
		}
	}
	return kept
}

func (e *Engine) collectOne(ctx context.Context, root *os.Root, state State, sources []string, op Operation) Item {
	item := Item{ID: op.ID, Status: "ok"}
	if op.Kind == "git" {
		return e.collectGit(ctx, state, op.ID)
	}
	match := func(line string) bool { return strings.Contains(line, op.Pattern) }
	if op.Regex {
		r, _ := regexp.Compile(op.Pattern) // validated before workers start
		match = r.MatchString
	}
	found := false
	resultBytes := 0
	for _, name := range sources {
		if ctx.Err() != nil {
			break
		}
		if op.Kind == "read" && name != op.Path || op.Kind == "search" && op.Path != "" && op.Path != "." && name != op.Path && !strings.HasPrefix(name, op.Path+"/") {
			continue
		}
		found = true
		data, err := readText(root, name)
		if err != nil {
			item.Skipped++
			item.Status, item.Error = "partial", "some sources could not be read as regular UTF-8 text of at most 1 MiB"
			if op.Kind == "read" || name == op.Path {
				item.Status, item.Error = "error", err.Error()
			}
			continue
		}
		lines, err := selectLines(name, string(data), op, match)
		if err != nil {
			return Item{ID: op.ID, Status: "error", Error: err.Error()}
		}
		for _, line := range lines {
			resultBytes += len(line.Text) + len(name) + 64
		}
		if resultBytes > MaxFileBytes {
			return Item{ID: op.ID, Status: "error", Error: "operation results exceed 1 MiB; narrow the path, range or pattern"}
		}
		item.Lines = append(item.Lines, lines...)
	}
	if !found && op.Path != "" {
		item.Status, item.Error = "error", "path is absent, ignored, a symlink or outside the repository sources"
	}
	return item
}

func selectLines(name, data string, op Operation, match func(string) bool) ([]Line, error) {
	var lines []Line
	retained := 0
	for n, line := range strings.SplitAfter(data, "\n") {
		if line == "" {
			continue
		}
		selected := n+1 >= max(1, op.Start) && (op.End == 0 || n+1 <= op.End)
		if op.Kind == "search" {
			selected = match(line)
		}
		if selected {
			parts := Lines(name, n+1, line)
			retained += len(line) + len(parts)*(len(name)+64)
			if retained > MaxFileBytes {
				return nil, errors.New("operation results exceed 1 MiB; narrow the path, range or pattern")
			}
			lines = append(lines, parts...)
		}
	}
	return lines, nil
}

func (e *Engine) collectGit(ctx context.Context, state State, id string) Item {
	item := Item{ID: id, Status: "ok"}
	data, _ := json.Marshal(state)
	item.Lines = []Line{{Number: 0, Text: string(data)}}
	if state.Head == "" {
		return item
	}
	changes, err := e.repo.Diff(ctx, state.Head, state.Tree)
	if err != nil {
		item.Status, item.Error = "error", err.Error()
		return item
	}
	for i, change := range changes {
		item.Lines = append(item.Lines, Line{Path: change.Path, Number: i + 1, Text: change.Kind})
	}
	return item
}

func readText(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil, errors.New("source is not a regular file of at most 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil || len(data) > MaxFileBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("source is not readable UTF-8 text without NUL bytes, of at most 1 MiB")
	}
	return data, nil
}

// Lines splits very long lines at UTF-8 boundaries so expansion can always progress.
func Lines(name string, number int, text string) []Line {
	text = strings.ToValidUTF8(text, "\ufffd")
	var lines []Line
	for offset := 0; len(text) > 0; {
		n := min(1024, len(text))
		for n > 0 && n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		lines = append(lines, Line{Path: name, Number: number, Offset: offset, Text: text[:n]})
		offset, text = offset+n, text[n:]
	}
	return lines
}
