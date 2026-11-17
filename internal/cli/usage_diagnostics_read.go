package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/insights"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

// All diagnostic reads pin existing directories and regular files. In
// particular they never acquire the stage ledger's creating/chmodding lock.
func diagnosticDirectory(parent *os.Root, name string) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe diagnostic directory %s", name)
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("diagnostic directory %s changed while opening", name)
	}
	return root, nil
}

func readDiagnosticJSON(root *os.Root, name string, value any) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > contextpack.MaxArtifact {
		return fmt.Errorf("unsafe or oversized diagnostic record %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("diagnostic record %s changed while opening", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, contextpack.MaxArtifact+1))
	if err != nil {
		return err
	}
	if len(data) > contextpack.MaxArtifact {
		return errors.New("diagnostic record exceeds read limit")
	}
	if err := uniqueReturnKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("diagnostic record contains trailing data")
	}
	return nil
}

func collectDiagnosticStore(s *session, input *insights.DiagnosticInput) error {
	git, err := os.OpenRoot(s.repo.CommonDir)
	if err != nil {
		return err
	}
	defer func() { _ = git.Close() }()
	root, err := diagnosticDirectory(git, "kitsu")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := collectDiagnosticCategory(root, s.store.Dir, "tokens", 1, input, collectDiagnosticStart); err != nil {
		return err
	}
	return collectDiagnosticCategory(root, s.store.Dir, "runs", 2, input, collectDiagnosticCheck)
}

type diagnosticRecordReader func(*os.Root, string, string, *insights.DiagnosticInput) error

func collectDiagnosticCategory(root *os.Root, storeDir, category string, depth int, input *insights.DiagnosticInput, read diagnosticRecordReader) error {
	dir, err := diagnosticDirectory(root, category)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return walkDiagnosticRecords(dir, "", depth, func(name string) error {
		source := filepath.Join(storeDir, category, filepath.FromSlash(name))
		return read(dir, name, source, input)
	})
}

func collectDiagnosticStart(root *os.Root, name, source string, input *insights.DiagnosticInput) error {
	var start tokens.Start
	err := readDiagnosticJSON(root, name, &start)
	if err != nil || start.Version != 1 || start.Session == "" || start.Agent == "" {
		input.DamagedTokens = append(input.DamagedTokens, source)
		return nil
	}
	input.Starts = append(input.Starts, start)
	return nil
}

func collectDiagnosticCheck(root *os.Root, name, source string, input *insights.DiagnosticInput) error {
	var record store.Record
	if err := readDiagnosticJSON(root, name, &record); err != nil {
		return fmt.Errorf("check record %s: %w", source, err)
	}
	if record.ID == "" || record.RunID == "" || record.ContentKey == "" || len(record.Run) == 0 || record.Outcome != "" && record.EndedAt == nil {
		return fmt.Errorf("incomplete check record %s", source)
	}
	if record.ID+"/"+record.ContentKey+"/"+record.RunID+".json" != name {
		return fmt.Errorf("check record identity differs from its original path: %s", source)
	}
	switch record.Outcome {
	case "", store.Passed, store.Failed, store.TimedOut, store.Interrupted, store.SourceChanged:
	default:
		return fmt.Errorf("check record %s has an unknown outcome", source)
	}
	input.Checks = append(input.Checks, insights.CheckRun{Record: record, Source: source})
	return nil
}

func walkDiagnosticRecords(root *os.Root, dir string, depth int, visit func(string) error) error {
	entries, err := fs.ReadDir(root.FS(), defaultDiagnosticDir(dir))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := path.Join(dir, entry.Name())
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if depth > 0 {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe diagnostic record directory %s", name)
			}
			if err := walkDiagnosticRecords(root, name, depth-1, visit); err != nil {
				return err
			}
		} else if strings.HasSuffix(entry.Name(), ".json") {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func defaultDiagnosticDir(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}
