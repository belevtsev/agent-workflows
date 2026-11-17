package insights

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/change"
)

const maxNoteBytes = 1 << 20

// NoteSource identifies the unchanged original, including unrecognized prose.
type NoteSource struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
	Format string `json:"format"`
}

// Note is a bounded projection of author-written claims, not established facts.
type Note struct {
	Kind          string            `json:"kind"`
	Path          string            `json:"path"`
	Line          int               `json:"line"`
	Fields        map[string]string `json:"fields"`
	Missing       []string          `json:"missing"`
	OmittedFields []string          `json:"omitted_fields"`
}

// Notes makes omissions and unsupported prose visible; originals remain authoritative.
type Notes struct {
	Authority    string       `json:"authority"`
	Sources      []NoteSource `json:"sources"`
	Items        []Note       `json:"items"`
	Closed       int          `json:"closed_rows"`
	Omitted      int          `json:"omitted_rows"`
	Problems     []string     `json:"problems"`
	Observations []Note       `json:"-"`
}

type noteConvention struct {
	file, heading, kind string
	columns             []string
}

var noteConventions = []noteConvention{
	{"proposal.md", "Consequential claims", "claim", []string{"claim", "kind", "source", "consequence if wrong", "revisit when", "status"}},
	{"design.md", "Consequential claims", "claim", []string{"claim", "kind", "source", "consequence if wrong", "revisit when", "status"}},
	{"design.md", "Decision notes", "decision", []string{"choice", "alternatives", "reason", "source", "revisit when"}},
	{"tests.md", "Troubleshooting", "hypothesis", []string{"id", "check", "content key", "policy key", "hypothesis", "prediction", "run", "observation", "status"}},
}

// ReadNotes reads optional conventional tables from existing full-task records.
// Unknown formats remain readable original references; no table is mandatory.
func ReadNotes(directory, task string, maxItems int) (Notes, error) {
	out := Notes{Authority: "author-notes; sources, observations and confirmed statuses are claims, not machine-certified truth", Sources: []NoteSource{}, Items: []Note{}, Problems: []string{}}
	if !noteTask(task) || maxItems < 1 || maxItems > 100 {
		return out, errors.New("notes require a valid task and item bound from 1 to 100")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return out, err
	}
	defer func() { _ = root.Close() }()
	for _, convention := range noteConventions {
		name := path.Join(change.Dir, task, convention.file)
		data, err := readNoteFile(root, name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return out, err
		}
		section := change.ReadOptionalSection(data, convention.heading)
		if section == nil {
			continue
		}
		digest := sha256.Sum256(data)
		format := "table"
		if len(section.Headers) == 0 {
			format = "unrecognized-prose"
		}
		out.Sources = append(out.Sources, NoteSource{Path: name, Line: section.Line, SHA256: hex.EncodeToString(digest[:]), Kind: convention.kind, Format: format})
		if format != "table" {
			out.Problems = append(out.Problems, name+": optional section has no recognized table; inspect the original")
		}
		for _, row := range section.Rows {
			appendNote(&out, convention, name, section.Headers, row, maxItems)
		}
	}
	return out, nil
}

func appendNote(out *Notes, convention noteConvention, name string, headers []string, row change.OptionalRow, maxItems int) {
	item := Note{Kind: convention.kind, Path: name, Line: row.Line, Fields: map[string]string{}, Missing: []string{}, OmittedFields: []string{}}
	seen := map[string]bool{}
	for i, header := range headers {
		key := strings.ToLower(strings.Trim(strings.TrimSpace(header), "`"))
		recognized := false
		for _, column := range convention.columns {
			if key == column {
				recognized = true
				break
			}
		}
		if !recognized {
			continue
		}
		if i >= len(row.Cells) {
			continue
		}
		if seen[key] {
			delete(item.Fields, key)
			item.OmittedFields = append(item.OmittedFields, key)
			continue
		}
		seen[key] = true
		value := row.Cells[i]
		if len(value) > 512 {
			item.OmittedFields = append(item.OmittedFields, key)
			continue
		}
		item.Fields[key] = value
	}
	for _, column := range convention.columns {
		if item.Fields[column] == "" {
			item.Missing = append(item.Missing, column)
		}
	}
	if item.Kind == "hypothesis" && item.Fields["observation"] != "" && item.Fields["run"] != "" {
		if len(out.Observations) < 4096 {
			out.Observations = append(out.Observations, item)
		} else if len(out.Problems) == 0 {
			out.Problems = append(out.Problems, "observation reference bound reached; inspect original notes")
		}
	}
	switch strings.ToLower(strings.Trim(item.Fields["status"], "` ")) {
	case "resolved", "closed", "rejected", "falsified":
		out.Closed++
		return
	}
	if len(out.Items) >= maxItems {
		out.Omitted++
		return
	}
	out.Items = append(out.Items, item)
}

func noteTask(task string) bool {
	if task == "" || len(task) > 128 || strings.HasPrefix(task, "-") || strings.HasSuffix(task, "-") || strings.Contains(task, "--") {
		return false
	}
	for _, c := range task {
		if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func readNoteFile(root *os.Root, name string) ([]byte, error) {
	part := ""
	for _, component := range strings.Split(path.Dir(name), "/") {
		part = path.Join(part, component)
		info, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("note directory %s must be real", part)
		}
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxNoteBytes {
		return nil, errors.New("note source is not a bounded regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("note source changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxNoteBytes+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	named, nameErr := root.Lstat(name)
	if err != nil || nameErr != nil || !os.SameFile(info, named) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("note source changed while reading")
	}
	if len(data) > maxNoteBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return nil, errors.New("note source is too large or not valid UTF-8 text")
	}
	return data, nil
}
