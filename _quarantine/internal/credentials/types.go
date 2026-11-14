// Package credentials stores only the three adviser credentials in a private
// per-device file. Credential values are never part of its JSON result types.
package credentials

import (
	"errors"
	"os"
	"unicode"
	"unicode/utf8"
)

const (
	TYPESAFE_API_KEY = "TYPESAFE_API_KEY"
	SUBQ_API_KEY     = "SUBQ_API_KEY"
	OPENAI_API_KEY   = "OPENAI_API_KEY"
)

var credentialNames = [...]string{TYPESAFE_API_KEY, SUBQ_API_KEY, OPENAI_API_KEY}

// Names is a copy of the supported public names, not the validation authority.
var Names = []string{TYPESAFE_API_KEY, SUBQ_API_KEY, OPENAI_API_KEY}

var (
	ErrUnsafeStore            = errors.New("credential storage is unsafe; use an owner-only physical directory and regular private files")
	ErrInvalidStore           = errors.New("credential storage has invalid contents")
	ErrInvalidChanges         = errors.New("credential changes contain an unsupported name or invalid value")
	ErrRead                   = errors.New("credential storage could not be read")
	ErrSave                   = errors.New("credential changes could not be saved")
	ErrConflict               = errors.New("credential storage changed; read the current credentials before saving again")
	ErrBusy                   = errors.New("credential storage is busy; no changes were committed")
	ErrLockUnsupported        = errors.New("credential storage is unavailable: file locking is not supported on this platform; no changes were committed")
	ErrPersistenceUnconfirmed = errors.New("credentials were committed but persistence durability is unconfirmed")
)

const maxDocumentBytes = 1 << 20
const maxKeyBytes = 64 << 10
const documentName = "credentials.json"
const lockName = ".credentials.lock"

// Store binds all reads and writes to one private configuration directory.
// SyncDirectory is an optional seam for the post-rename durability operation.
type Store struct {
	Directory     string
	SyncDirectory func(*os.File) error
}

// Snapshot's generation is the exact file-content digest, or empty for a missing
// file. Neither credentials nor their generation are included in JSON output.
type Snapshot struct {
	Keys       map[string]string `json:"-"`
	Generation string            `json:"-"`
}

// SaveResult separates a changed request, completed rename and verified durable
// persistence. A post-rename failure must never be described as an unsaved key.
type SaveResult struct {
	Changed            bool `json:"changed"`
	Committed          bool `json:"committed"`
	DurabilityVerified bool `json:"durability_verified"`
}

type Resolver struct {
	Directory string
	Getenv    func(string) string
}

type Presence struct {
	Environment bool   `json:"environment"`
	Stored      bool   `json:"stored"`
	Effective   bool   `json:"effective"`
	Source      string `json:"source"`
}

func knownName(name string) bool {
	switch name {
	case TYPESAFE_API_KEY, SUBQ_API_KEY, OPENAI_API_KEY:
		return true
	default:
		return false
	}
}

// ValidKey reports whether value can be saved as an adviser key: 1 byte to
// 64 KiB of UTF-8 text without whitespace or control characters. cw keys set
// and the store apply this one rule.
func ValidKey(value string) bool {
	if len(value) == 0 || len(value) > maxKeyBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return false
		}
	}
	return true
}
