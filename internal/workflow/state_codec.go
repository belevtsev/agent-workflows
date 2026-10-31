package workflow

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
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

// OwnershipRecord is the typed boundary for the sealed ownership document. The
// original JSON object is retained for its canonical integrity seal.
type OwnershipRecord struct {
	Version               int                   `json:"version"`
	Release               string                `json:"release"`
	Source                string                `json:"source,omitempty"`
	Home                  string                `json:"home"`
	StateDir              string                `json:"state_dir"`
	ManifestRegistrations map[string]string     `json:"manifest_registrations"`
	Hosts                 map[string]HostRecord `json:"hosts"`
	History               []ActivationRecord    `json:"history"`
	Manager               *ManagerRecord        `json:"manager,omitempty"`
	Integrity             string                `json:"integrity_sha256"`
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

type ActivationRecord struct {
	Release string `json:"release"`
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

// JournalRecord validates envelope field types before any recovery planning.
// Operation validation remains discriminated by kind and constrained to owned
// paths.
type JournalRecord struct {
	Version            int               `json:"version"`
	Command            string            `json:"command"`
	Home               string            `json:"home"`
	CodexHome          string            `json:"codex_home"`
	ClaudeHome         string            `json:"claude_home"`
	StateDir           string            `json:"state_dir"`
	Operations         []OperationRecord `json:"operations"`
	RecoveryOperations []OperationRecord `json:"recovery_operations,omitempty"`
	Integrity          string            `json:"integrity_sha256"`
	RecoveryRuntime    *RuntimeIdentity  `json:"recovery_runtime,omitempty"`
}

func decodeOwnership(value Object) (OwnershipRecord, error) {
	var record OwnershipRecord
	if err := json.Unmarshal(legacyJSON(value), &record, json.RejectUnknownMembers(true)); err != nil {
		return record, fmt.Errorf("invalid ownership record: %w", err)
	}
	if record.Version != 3 || len(record.Hosts) == 0 || record.ManifestRegistrations == nil || record.History == nil {
		return record, errors.New("unsupported or incomplete ownership record")
	}
	for id, host := range record.Hosts {
		if (id != hostCodex && id != hostClaude) || host.Registrations == nil || host.GlobalOrigin == nil {
			return record, errors.New("unsupported or incomplete host ownership record")
		}
	}
	if record.Source != "" && Normalize(record.Source) != record.Source {
		return record, errors.New("invalid recorded source path")
	}
	return record, nil
}

func readOwnership(path string) (Object, error) {
	data, err := readRecordBytes(path)
	if err != nil {
		return nil, err
	}
	var record OwnershipRecord
	if err = json.Unmarshal(data, &record, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("invalid ownership record: %w", err)
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
	var record JournalRecord
	if err = json.Unmarshal(data, &record, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("invalid journal record: %w", err)
	}
	var value Object
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeJournal(value Object) error {
	var record JournalRecord
	if err := json.Unmarshal(legacyJSON(value), &record, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("invalid journal record: %w", err)
	}
	if record.Version != 3 || record.Operations == nil {
		return errors.New("unsupported or incomplete journal record")
	}
	switch record.Command {
	case "install", "setup", "update", "rollback", "uninstall":
	default:
		return errors.New("unknown journal command")
	}
	return nil
}
