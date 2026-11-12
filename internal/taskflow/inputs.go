package taskflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// MaxBriefInputs bounds the inputs one worker brief may carry.
const MaxBriefInputs = 32

// InputRef names one artefact the coordinator hands a worker: a regular file,
// inside or outside the project, with the SHA-256 of its bytes.
type InputRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

var inputFileName = regexp.MustCompile(`^input-[A-Za-z][A-Za-z0-9_-]{0,63}-[0-9a-f]{32}$`)

// lowerSHA256 reports whether value is a SHA-256 in lowercase hexadecimal.
func lowerSHA256(value string) bool {
	return validSHA256(value) && strings.ToLower(value) == value
}

// validateInputs checks the shape of a brief's inputs: at most MaxBriefInputs
// clean absolute paths, each with its SHA-256, and no path given twice.
func validateInputs(inputs []InputRef) error {
	if len(inputs) > MaxBriefInputs {
		return fmt.Errorf("worker brief inputs need at most %d clean absolute paths, each with its SHA-256", MaxBriefInputs)
	}
	for _, input := range inputs {
		if !filepath.IsAbs(input.Path) || filepath.Clean(input.Path) != input.Path || len(input.Path) > 4096 || strings.ContainsFunc(input.Path, unicode.IsControl) || !lowerSHA256(input.SHA256) {
			return fmt.Errorf("worker brief inputs need at most %d clean absolute paths, each with its SHA-256", MaxBriefInputs)
		}
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		if seen[input.Path] {
			return fmt.Errorf("worker brief inputs repeat %s", input.Path)
		}
		seen[input.Path] = true
	}
	return nil
}

// readInput reads one brief input and checks its bytes against its SHA-256.
func readInput(input InputRef) ([]byte, error) {
	data, err := readInputFile("input "+input.Path, input.Path)
	if err != nil {
		return nil, err
	}
	if digest(data) != input.SHA256 {
		return nil, fmt.Errorf("input %s sha256 differs from the file's bytes", input.Path)
	}
	return data, nil
}

// verifyInputs checks every input's file, size and hash, in order, without
// storing anything.
func verifyInputs(inputs []InputRef) error {
	for _, input := range inputs {
		if _, err := readInput(input); err != nil {
			return err
		}
	}
	return nil
}

// storeInputs stores a private copy of each input of brief in the task
// directory, returning the copies' names in brief order. Callers run
// verifyInputs first; each input is read again here, so that at most one is
// held in memory, and checked against the same hash. When an input fails here
// (its file changed since verifyInputs, or a write failed) the copies already
// written are removed. A failed removal leaves its copy, and so does a write
// that linked the copy but then failed to sync the directory, since that
// copy's name is not returned; a crash or kill before the assignment's
// revision is published leaves every copy written so far. Copies returned
// here are removed by AssignWorker when the record update then fails, within
// the limits updateDiscarding states.
func storeInputs(root *os.Root, brief WorkerBrief) (names []string, err error) {
	defer func() {
		if err != nil {
			for _, name := range names {
				root.Remove(name)
			}
			names = nil
		}
	}()
	for _, input := range brief.Inputs {
		data, err := readInput(input)
		if err != nil {
			return names, err
		}
		id, err := randomID()
		if err != nil {
			return names, err
		}
		name := "input-" + brief.ID + "-" + id
		if err := writeNew(root, name, data); err != nil {
			return names, err
		}
		names = append(names, name)
	}
	return names, nil
}

// validateInputFiles checks a stored assignment's input copy names: one
// distinct name per brief input.
func validateInputFiles(assigned WorkerAssignment) error {
	if len(assigned.InputFiles) != len(assigned.Brief.Inputs) {
		return errors.New("stored assignment input binding is invalid")
	}
	seen := map[string]bool{}
	for _, name := range assigned.InputFiles {
		if !inputFileName.MatchString(name) || seen[name] {
			return errors.New("stored assignment input binding is invalid")
		}
		seen[name] = true
	}
	return nil
}

// inputCopyProblems lists, in assignment and input order, every stored input
// copy that is missing or no longer hashes to its input's SHA-256.
func inputCopyProblems(artifacts Artifacts, record Record) []string {
	problems := []string{}
	for _, assigned := range record.Assignments {
		for i, input := range assigned.Brief.Inputs {
			if i < len(assigned.InputFiles) {
				if data, err := artifacts.Read(assigned.InputFiles[i], MaxInputBytes); err == nil && digest(data) == input.SHA256 {
					continue
				}
			}
			problems = append(problems, fmt.Sprintf("worker assignment %s input %s: stored copy is missing or differs from its sha256", assigned.Brief.ID, input.Path))
		}
	}
	return problems
}
