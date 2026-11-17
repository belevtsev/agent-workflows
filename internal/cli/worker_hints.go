package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/decl"
)

const maxWorkerHintIDBytes = 256 // Aggregate serialized operands in quiet and noisy recipes.

// Hints are immutable observations and argv templates, never execution authority.
// Only preparation snapshots them; policy verification must reuse the saved bytes.
type workerHints struct {
	Recipes  map[string][]string `json:"argv"`
	RG       workerToolLookup    `json:"rg_lookup"`
	Guidance string              `json:"guidance"`
}

func snapshotWorkerHints(s *session, b workerBrief) (*workerHints, error) {
	if b.Role != "quality-engineer" {
		return nil, nil
	}
	ids := workerHintCheckIDs(s.repo.Root)
	commands := []struct {
		name, path string
		operands   []string
		options    [][2]string
	}{
		{name: "quiet", path: "check run", operands: ids},
		{name: "noisy", path: "check run", operands: ids, options: [][2]string{{"compact", ""}}},
		{name: "original_log", path: "context show", operands: []string{"<artifact-id>"}, options: [][2]string{{"item", "<item-id>"}}},
		{name: "continue_log", path: "context show", operands: []string{"<artifact-id>"}, options: [][2]string{{"item", "<item-id>"}, {"cursor", "<next_cursor>"}}},
		{name: "search_fallback", path: "context search", operands: []string{"--", "<literal>", "<path>"}},
	}
	hints := &workerHints{
		Recipes: map[string][]string{},
		RG:      observeWorkerTool(workerLookupScope(b.Host, s.repo.Root)),
		Guidance: "Substitute <...> from the response or scoped search. quiet/noisy select all declared checks; use ordinary output for quiet checks. " +
			"Expand only omitted needed evidence: original_log starts at the retained original, continue_log uses its next_cursor; never rerun to expand. " +
			"Respect permissions; group dependent format/check calls only on success. Use bounded waits with cancellation/deadlines. " +
			"Send required progress/questions; use the native final return without a duplicate completion message.",
	}
	for _, command := range commands {
		argv, err := workerHintArgv(command.path, command.operands, command.options)
		if err != nil {
			return nil, err
		}
		hints.Recipes[command.name] = argv
	}
	return hints, nil
}

// Large or unreadable declarations use the CLI's all-declared-checks operand
// default, rather than publishing an incomplete suite or an unbounded inventory.
func workerHintCheckIDs(root string) []string {
	data, err := uxCompletionRead(filepath.Join(root, decl.FileName))
	if err != nil {
		return nil
	}
	file, err := decl.Parse(data)
	if err != nil || len(file.Checks) > 8 {
		return nil
	}
	ids := make([]string, 0, len(file.Checks))
	for _, check := range file.Checks {
		ids = append(ids, check.ID)
	}
	operands, _ := json.Marshal(ids)
	if 2*len(operands) > maxWorkerHintIDBytes {
		return nil
	}
	return ids
}

// Use the same command/flag metadata as help and dispatch. A removed command or
// changed flag fails preparation instead of silently saving obsolete syntax.
func workerHintArgv(path string, operands []string, options [][2]string) ([]string, error) {
	for _, command := range uxCommands() {
		if command.path != path {
			continue
		}
		argv := append([]string{"kitsu"}, strings.Fields(command.path)...)
		for _, option := range options {
			flag, ok := uxCompletionFlag(command, option[0])
			if !ok || (flag.value == "") != (option[1] == "") {
				return nil, fmt.Errorf("worker recipe metadata changed: %s --%s", path, option[0])
			}
			argv = append(argv, "--"+flag.name)
			if flag.value != "" {
				argv = append(argv, option[1])
			}
		}
		return append(argv, operands...), nil
	}
	return nil, fmt.Errorf("worker recipe command removed: %s", path)
}

// Fingerprints avoid copying local host names, environment paths or session ids
// into a packet. Missing session identity remains explicitly unknown.
func workerLookupScope(host, worktree string) workerToolScope {
	hostname, err := os.Hostname()
	machine := ""
	if err == nil && hostname != "" {
		machine = contextpack.Key([]string{hostname, strconv.Itoa(os.Getuid())})
	}
	session := []string{}
	for _, name := range []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDE_SESSION_ID"} {
		if value := os.Getenv(name); value != "" {
			session = append(session, name+"="+value)
		}
	}
	sessionKey := ""
	if len(session) > 0 {
		sessionKey = contextpack.Key(session)
	}
	return newWorkerToolScope(host, worktree, machine, sessionKey)
}

func workerLookupState(err error) string {
	if err == nil {
		return "available"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	return "unknown"
}
