package workflow

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// VerifyLocalSeal checks the exact historical local-document checksum encoding.
// Runtime discovery shares it so locator verification does not invent a second
// canonical JSON representation.
func VerifyLocalSeal(data []byte) error {
	var value Object
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	return verifySeal(value)
}

// HostRecord holds what one agent host owns. Settings metadata differs per host:
// Codex keeps its config.toml groups, and Claude Code keeps settings.json keys.
type HostRecord struct {
	Home                 string                        `json:"home"`
	Registrations        map[string]RegistrationRecord `json:"registrations"`
	GlobalOrigin         *GlobalOriginRecord           `json:"global_origin"`
	GlobalSegment        string                        `json:"global_segment"`
	ModelConfig          *ModelMetadataRecord          `json:"model_config,omitempty"`
	SharedConfig         *ModelMetadataRecord          `json:"shared_config,omitempty"`
	ConfigOriginalExists *bool                         `json:"config_original_exists,omitempty"`
	Settings             *ModelMetadataRecord          `json:"settings,omitempty"`
}

type RegistrationRecord struct {
	Target   string        `json:"target"`
	Original *OriginRecord `json:"original"`
}

type GlobalOriginRecord struct {
	Kind string `json:"kind"`
	Mode *int   `json:"mode,omitempty"`
}

type OriginRecord struct {
	Kind      string                   `json:"kind"`
	Target    string                   `json:"target,omitempty"`
	Path      string                   `json:"path,omitempty"`
	Backup    string                   `json:"backup,omitempty"`
	Inventory []PersonalInventoryEntry `json:"inventory,omitempty"`
}

type ObservationRecord struct {
	Kind      string                     `json:"kind"`
	Target    string                     `json:"target,omitempty"`
	Data      string                     `json:"data,omitempty"`
	Mode      *int                       `json:"mode,omitempty"`
	Inventory map[string]InventoryRecord `json:"inventory,omitempty"`
}

type InventoryRecord struct {
	Kind   string `json:"kind"`
	Mode   int    `json:"mode"`
	SHA256 string `json:"sha256,omitempty"`
}

type ModelKeyRecord struct {
	Present        bool   `json:"present"`
	Representation string `json:"representation,omitempty"`
}

type ModelMetadataRecord struct {
	Original       map[string]ModelKeyRecord `json:"original"`
	Expected       map[string]ModelKeyRecord `json:"expected"`
	OriginalExists bool                      `json:"original_exists"`
}

// OperationRecord is a typed union at the recovery boundary. Discriminated
// validators enforce each kind's exact field set and permitted owned paths.
type OperationRecord struct {
	Kind          string                        `json:"kind"`
	Path          string                        `json:"path"`
	Label         string                        `json:"label"`
	Before        *ObservationRecord            `json:"before,omitempty"`
	After         *ObservationRecord            `json:"after,omitempty"`
	Shell         string                        `json:"shell,omitempty"`
	Source        string                        `json:"source,omitempty"`
	BeforeSegment *string                       `json:"before_segment,omitempty"`
	AfterSegment  *string                       `json:"after_segment,omitempty"`
	BeforeExists  *bool                         `json:"before_exists,omitempty"`
	AfterExists   *bool                         `json:"after_exists,omitempty"`
	BeforeMode    *int                          `json:"before_mode,omitempty"`
	AfterMode     *int                          `json:"after_mode,omitempty"`
	SourceSegment *string                       `json:"source_segment,omitempty"`
	TargetSegment *string                       `json:"target_segment,omitempty"`
	Restoration   *OperationRecord              `json:"restoration,omitempty"`
	BeforeKeys    map[string]ModelKeyRecord     `json:"before_keys,omitempty"`
	AfterKeys     map[string]ModelKeyRecord     `json:"after_keys,omitempty"`
	Personal      *PersonalSkillOperationRecord `json:"personal,omitempty"`
}

// ErrLegacyInstallation reports ownership state or a journal written by a cw
// that predates the standalone manager. This release reads neither.
var ErrLegacyInstallation = errors.New(`this installation was made by an older cw that this release no longer supports; run "cw uninstall" with your existing cw, then install again`)

// decodeOwnership is the boundary every strict state reader uses. Versions one
// to three predate the standalone manager. A version-four state that names a
// source checkout or a commit-SHA release in its history was produced by the
// removed migration from version three, and is refused the same way.
func decodeOwnership(value Object) (standaloneOwnershipRecord, error) {
	if v := integer(value["version"]); v >= 1 && v <= 3 {
		return standaloneOwnershipRecord{}, ErrLegacyInstallation
	}
	if integer(value["version"]) == 4 {
		if text(value["source"]) != "" {
			return standaloneOwnershipRecord{}, ErrLegacyInstallation
		}
		for _, raw := range sequence(value["history"]) {
			if commitSHA.MatchString(text(object(raw)["release"])) {
				return standaloneOwnershipRecord{}, ErrLegacyInstallation
			}
		}
	}
	return decodeStandaloneOwnership(value)
}

// legacyRecords returns ErrLegacyInstallation when the state root holds a
// journal or ownership state from an older cw. Lifecycle commands call it
// before anything else so the refusal precedes every write. Other read
// failures are not this check's to report: the strict reads that follow
// return them.
func legacyRecords(stateDir string) error {
	if _, err := readJournalRecord(filepath.Join(stateDir, "journal.json")); errors.Is(err, ErrLegacyInstallation) {
		return err
	}
	if _, err := readOwnership(filepath.Join(stateDir, "state.json")); errors.Is(err, ErrLegacyInstallation) {
		return err
	}
	return nil
}

func readOwnership(path string) (Object, error) {
	data, err := readRecordBytes(path)
	if err != nil {
		return nil, err
	}
	var value Object
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if _, err = decodeOwnership(value); err != nil {
		return nil, err
	}
	return value, nil
}

func readRecordBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe ownership document: %s", path)
	}
	return os.ReadFile(path)
}

func readJournalRecord(path string) (Object, error) {
	data, err := readRecordBytes(path)
	if err != nil {
		return nil, err
	}
	var value Object
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if err = decodeJournal(value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeJournal(value Object) error {
	if v := integer(value["version"]); v >= 1 && v <= 3 {
		return ErrLegacyInstallation
	}
	return decodeStandaloneJournal(value)
}
