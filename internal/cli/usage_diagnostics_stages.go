package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/insights"
)

type diagnosticStageRecords struct {
	prepared map[string]stageRecord
	receipts map[string][]stageReceipt
	sources  map[string][]string
	active   string
}

func collectDiagnosticStages(s *session, input *insights.DiagnosticInput) error {
	git, err := os.OpenRoot(s.repo.GitDir)
	if err != nil {
		return err
	}
	defer func() { _ = git.Close() }()
	root, err := diagnosticDirectory(git, "kitsu-stages")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	records := diagnosticStageRecords{prepared: map[string]stageRecord{}, receipts: map[string][]stageReceipt{}, sources: map[string][]string{}}
	for _, entry := range entries {
		if err := records.read(root, entry.Name(), s, input); err != nil {
			return err
		}
	}
	return records.project(input, s.repo.GitDir)
}

func (records *diagnosticStageRecords) read(root *os.Root, name string, s *session, input *insights.DiagnosticInput) error {
	if !strings.HasSuffix(name, ".json") {
		return nil
	}
	switch {
	case name == "active.json":
		if err := readDiagnosticJSON(root, name, &records.active); err != nil {
			return err
		}
		if !stageValidID(records.active) {
			return errors.New("invalid diagnostic active stage pointer")
		}
		return nil
	case strings.HasPrefix(name, "verification-"):
		return diagnosticVerification(root, name, input, s.repo.GitDir)
	case strings.HasSuffix(name, ".finish.json"), strings.HasSuffix(name, ".abandon.json"):
		return records.readReceipt(root, name)
	default:
		id := strings.TrimSuffix(name, ".json")
		record, err := readDiagnosticPrepared(root, name, id, s.repo.Root)
		if err != nil {
			return err
		}
		records.prepared[id] = record
		return nil
	}
}

func readDiagnosticPrepared(root *os.Root, name, id, worktree string) (stageRecord, error) {
	var record stageRecord
	if !stageValidID(id) {
		return record, fmt.Errorf("unknown stage record %s", name)
	}
	if err := readDiagnosticJSON(root, name, &record); err != nil {
		return record, fmt.Errorf("stage record %s: %w", name, err)
	}
	if record.Version != 1 || contextpack.Key(record) != id || record.Worktree != worktree || record.Created.IsZero() || record.Created.After(time.Now().Add(time.Minute)) || record.Prepared.Brief.Task == "" {
		return record, fmt.Errorf("stage record %s is incomplete or changed", name)
	}
	return record, nil
}

func (records *diagnosticStageRecords) readReceipt(root *os.Root, name string) error {
	var receipt stageReceipt
	if err := readDiagnosticJSON(root, name, &receipt); err != nil {
		return fmt.Errorf("stage receipt %s: %w", name, err)
	}
	id := receipt.ID
	receipt.ID = ""
	if receipt.Version != 1 || !stageValidID(receipt.DispatchID) || !stageValidID(id) || contextpack.Key(receipt) != id {
		return fmt.Errorf("stage receipt %s is incomplete or changed", name)
	}
	receipt.ID = id
	expected := stageReceiptName(stageFinishOptions{id: receipt.DispatchID, abandon: receipt.Status == "abandoned"})
	if name != expected {
		return fmt.Errorf("stage receipt %s differs from its original identity", name)
	}
	records.receipts[receipt.DispatchID] = append(records.receipts[receipt.DispatchID], receipt)
	records.sources[receipt.DispatchID] = append(records.sources[receipt.DispatchID], name)
	return nil
}

func (records *diagnosticStageRecords) project(input *insights.DiagnosticInput, gitDir string) error {
	for id := range records.receipts {
		if _, exists := records.prepared[id]; !exists {
			return fmt.Errorf("stage receipts %v have no original preparation record", records.sources[id])
		}
	}
	if records.active != "" {
		if _, exists := records.prepared[records.active]; !exists {
			return errors.New("active stage pointer has no original preparation record")
		}
	}
	ids := make([]string, 0, len(records.prepared))
	for id := range records.prepared {
		ids = append(ids, id)
	}
	// Stable source order makes bounded samples consistent on repeated reads.
	slices.Sort(ids)
	for _, id := range ids {
		stage, err := records.stage(id, gitDir)
		if err != nil {
			return err
		}
		input.Stages = append(input.Stages, stage)
	}
	return nil
}

func (records *diagnosticStageRecords) stage(id, gitDir string) (insights.ManagedStage, error) {
	record := records.prepared[id]
	stage := insights.ManagedStage{ID: id, Task: record.Prepared.Brief.Task, Source: filepath.Join(gitDir, "kitsu-stages", id+".json"), Prepared: record.Created, Status: "unfinished"}
	receipts := records.receipts[id]
	if len(receipts) == 0 {
		return stage, nil
	}
	for i, receipt := range receipts {
		if err := validateStageReceiptSemantics(record, receipt); err != nil {
			return stage, fmt.Errorf("stage receipt %s: %w", records.sources[id][i], err)
		}
		source := filepath.Join(gitDir, "kitsu-stages", records.sources[id][i])
		stage.ReceiptSources = append(stage.ReceiptSources, source)
		// A durable finish may be followed by explicit abandonment of a stale
		// active pointer after interrupted cleanup. The later cleanup does not
		// extend the original managed interval or erase its historical evidence.
		if stage.Ended == nil || receipt.Created.Before(*stage.Ended) {
			ended := receipt.Created
			stage.Ended, stage.ReceiptSource = &ended, source
		}
		if stage.Status != "abandoned" {
			stage.Status = receipt.Status
		}
		for _, check := range receipt.Checks {
			stage.Checks = append(stage.Checks, insights.CheckReference{Task: stage.Task, CheckID: check.ID, RunID: check.RunID, Source: source})
		}
	}
	return stage, nil
}

func diagnosticVerification(root *os.Root, name string, input *insights.DiagnosticInput, gitDir string) error {
	task := strings.TrimSuffix(strings.TrimPrefix(name, "verification-"), ".json")
	if !taskName.MatchString(task) {
		return fmt.Errorf("invalid private verification filename %s", name)
	}
	var observation localVerification
	if err := readDiagnosticJSON(root, name, &observation); err != nil {
		return err
	}
	if observation.Version != 1 || observation.Results == nil || observation.Key != observation.fingerprint() || observation.Attempt == "" {
		return fmt.Errorf("private verification %s is incomplete or changed", name)
	}
	for _, result := range observation.Results {
		input.References = append(input.References, insights.CheckReference{Task: task, CheckID: result.ID, RunID: result.RunID, Source: filepath.Join(gitDir, "kitsu-stages", name)})
	}
	return nil
}
