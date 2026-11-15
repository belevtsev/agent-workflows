package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func uxCompleteValue(ctx context.Context, state uxCompleteState, flag uxFlag, prefix string) []uxSuggestion {
	if len(flag.choices) > 0 {
		return uxSuggestionValues(flag.choices, flag.description)
	}
	state.current = prefix
	name := strings.TrimLeft(flag.name, "-")
	switch name {
	case "host":
		return uxSuggestionValues(roles.Hosts, "Agent host")
	case "role":
		return uxCompletionRoles()
	case "task":
		return uxCompletionTasks(ctx)
	case "model":
		return uxCompletionModels(ctx, state)
	case "reasoning", "effort":
		return uxSuggestionValues([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, "Reasoning effort")
	case "skills":
		head, tail := "", prefix
		if index := strings.LastIndex(prefix, ","); index >= 0 {
			head, tail = prefix[:index+1], prefix[index+1:]
		}
		state.current = tail
		values := uxFilterSuggestions(uxCompletionSkills(ctx, state), tail)
		for i := range values {
			values[i].Value = head + values[i].Value
		}
		return values
	case "checkpoint":
		return uxCompletionCheckpoints(ctx, state)
	case "root":
		return uxCompletionPaths(prefix, true)
	case "request", "paths", "file", "path", "output":
		return uxCompletionPaths(prefix, false)
	case "base", "from":
		return []uxSuggestion{{"HEAD", "Current Git revision"}}
	case "workflow":
		return uxSuggestionValues([]string{"full", "small"}, "Task workflow")
	}
	return nil
}

func uxCompleteOperand(ctx context.Context, state uxCompleteState) []uxSuggestion {
	names := uxCompletionOperands(state.command)
	index := len(state.pos)
	if index >= len(names) {
		if len(names) == 0 || !strings.Contains(state.command.args, "...") {
			return nil
		}
		index = len(names) - 1
	}
	kind := names[index]
	switch kind {
	case "role":
		return uxCompletionRoles()
	case "task":
		return uxCompletionTasks(ctx)
	case "skill", "reference", "references":
		return uxCompletionSkills(ctx, state)
	case "adviser":
		return uxCompletionAdvisers(ctx, state)
	case "key":
		return []uxSuggestion{{"host", "Default CLI agent host"}}
	case "value":
		if len(state.pos) > 0 && state.pos[0] == "host" {
			return uxSuggestionValues(roles.Hosts, "Agent host")
		}
	case "id":
		if state.command.path == "check run" {
			return uxCompletionChecks(ctx)
		}
		if strings.HasPrefix(state.command.path, "adviser ") {
			return uxCompletionConsultations(ctx, state)
		}
	case "file", "path", "directory", "program", "argument":
		return uxCompletionPaths(state.current, kind == "directory")
	}
	return nil
}

func uxCompletionOperands(command uxCommand) []string {
	names := []string{}
	if len(command.operands) > 0 {
		for _, operand := range command.operands {
			names = append(names, strings.Trim(operand[0], "[]<>."))
		}
		return names
	}
	skip := false
	for _, token := range strings.Fields(command.args) {
		if token == "--" {
			continue
		}
		if strings.HasPrefix(token, "--") {
			skip = true
			continue
		}
		_, rest, found := strings.Cut(token, "<")
		name, _, closed := strings.Cut(rest, ">")
		if found && closed {
			if !skip {
				names = append(names, name)
			}
			skip = false
		}
	}
	return names
}

func uxCompletionRoles() []uxSuggestion {
	result := []uxSuggestion{{roles.Coordinator, "Coordinate the workflow in the current host session"}}
	for _, role := range roles.All(nil) {
		result = append(result, uxSuggestion{role.ID, role.Job})
	}
	return result
}

func uxCompletionHost(ctx context.Context, state uxCompleteState) string {
	if values := state.values["host"]; len(values) > 0 {
		return values[len(values)-1]
	}
	host, _, err := uxHostPreference(ctx)
	if err != nil {
		return ""
	}
	return host
}

func uxCompletionRole(state uxCompleteState) string {
	if values := state.values["role"]; len(values) > 0 {
		return values[len(values)-1]
	}
	return uxCompletionPositional(state, "role")
}

func uxCompletionTask(state uxCompleteState) string {
	if values := state.values["task"]; len(values) > 0 {
		return values[len(values)-1]
	}
	return uxCompletionPositional(state, "task")
}

func uxCompletionPositional(state uxCompleteState, name string) string {
	for i, kind := range uxCompletionOperands(state.command) {
		if kind == name && i < len(state.pos) {
			return state.pos[i]
		}
	}
	return ""
}

func uxCompletionRoot(ctx context.Context) string {
	root, err := skillWorkingRoot(ctx)
	if err != nil {
		return ""
	}
	return root
}

func uxCompletionModels(ctx context.Context, state uxCompleteState) []uxSuggestion {
	root, host := uxCompletionRoot(ctx), uxCompletionHost(ctx, state)
	configured, _ := roles.Load(root)
	result := []uxSuggestion{}
	for _, candidateHost := range roles.Hosts {
		if host == "" || host == candidateHost {
			result = append(result, uxSuggestionValues(uxModelSuggestions(configured, candidateHost, false), candidateHost+" role model")...)
		}
	}
	if strings.HasPrefix(state.command.path, "adviser ") {
		for _, preset := range advisers.Providers() {
			result = append(result, uxSuggestion{preset.Model, preset.ID + " provider model"})
		}
		if config, err := advisers.Load(root); err == nil {
			for name, definition := range config.Advisers {
				result = append(result, uxSuggestion{definition.Model, name + " configured adviser model"})
			}
		}
	}
	return result
}

func uxCompletionSkills(ctx context.Context, state uxCompleteState) []uxSuggestion {
	root, host := uxCompletionRoot(ctx), uxCompletionHost(ctx, state)
	if host != "codex" && host != "claude" {
		return nil
	}
	if state.command.path == "skill detach" {
		config, err := roles.Load(root)
		global := state.values["global"]
		personal, _ := strconv.ParseBool(uxCompletionLast(global))
		if personal {
			config, err = roles.LoadPersonal()
		}
		if err != nil {
			return nil
		}
		result, role := []uxSuggestion{}, uxCompletionRole(state)
		for name, refs := range config.Skills[host] {
			if role == "" || name == role {
				result = append(result, uxSuggestionValues(refs, name+" selected reference")...)
			}
		}
		return result
	}
	catalog, err := skills.DiscoverWithRoots(root, host, state.values["root"])
	if err != nil {
		return nil
	}
	counts := map[string]int{}
	for _, entry := range catalog.Skills {
		counts[entry.Name]++
	}
	result := []uxSuggestion{}
	for _, entry := range catalog.Skills {
		if state.command.path != "skill show" && (entry.Disabled || entry.ManualOnly) {
			continue
		}
		result = append(result, uxSuggestion{entry.ID, entry.Description})
		if counts[entry.Name] == 1 {
			result = append(result, uxSuggestion{entry.Name, entry.Description})
		}
	}
	return result
}

func uxCompletionLast(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func uxCompletionTasks(ctx context.Context) []uxSuggestion {
	root := uxCompletionRoot(ctx)
	if root == "" {
		return nil
	}
	tasks, _ := skills.Tasks(root)
	small, _ := smallAdviserTasks(root)
	return uxSuggestionValues(append(tasks, small...), "Saved task")
}

func uxCompletionAdvisers(ctx context.Context, state uxCompleteState) []uxSuggestion {
	root, result := uxCompletionRoot(ctx), []uxSuggestion{}
	if config, err := advisers.Load(root); err == nil {
		for name, definition := range config.Advisers {
			result = append(result, uxSuggestion{name, definition.Model})
		}
	}
	if task := uxCompletionTask(state); root != "" && taskName.MatchString(task) {
		if binding, err := advisers.LoadBinding(root, task); err == nil {
			for name, identity := range binding.Advisers {
				result = append(result, uxSuggestion{name, identity.Model})
			}
		}
	}
	return result
}

func uxCompletionCheckpoints(ctx context.Context, state uxCompleteState) []uxSuggestion {
	root, result := uxCompletionRoot(ctx), []uxSuggestion{}
	if task := uxCompletionTask(state); root != "" && taskName.MatchString(task) {
		if binding, err := advisers.LoadBinding(root, task); err == nil {
			for _, checkpoint := range binding.Checkpoints {
				result = append(result, uxSuggestion{checkpoint.ID, checkpoint.Stage + " checkpoint"})
			}
			return result
		}
	}
	if config, err := advisers.Load(root); err == nil {
		for _, checkpoint := range config.Checkpoints {
			result = append(result, uxSuggestion{checkpoint.ID, checkpoint.Stage + " checkpoint"})
		}
	}
	return result
}

func uxCompletionConsultations(ctx context.Context, state uxCompleteState) []uxSuggestion {
	root, task := uxCompletionRoot(ctx), uxCompletionTask(state)
	if root == "" || !taskName.MatchString(task) {
		return nil
	}
	result := []uxSuggestion{}
	if consultations, err := advisers.ReadConsultations(root, task); err == nil {
		for _, consultation := range consultations {
			result = append(result, uxSuggestion{consultation.ID, consultation.Adviser + " consultation"})
		}
	}
	return result
}

func uxCompletionChecks(ctx context.Context) []uxSuggestion {
	root := uxCompletionRoot(ctx)
	if root == "" {
		return nil
	}
	data, err := uxCompletionRead(filepath.Join(root, decl.FileName))
	if err != nil {
		return nil
	}
	declaration, err := decl.Parse(data)
	if err != nil {
		return nil
	}
	result := []uxSuggestion{}
	for _, check := range declaration.Checks {
		result = append(result, uxSuggestion{check.ID, "Declared repository check"})
	}
	return result
}

func uxCompletionPaths(prefix string, directoriesOnly bool) []uxSuggestion {
	lookup := prefix
	if strings.HasPrefix(prefix, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			lookup = filepath.Join(home, prefix[2:])
		}
	}
	dir, _ := filepath.Split(lookup)
	displayDir, base := filepath.Split(prefix)
	if dir == "" {
		dir = "."
	}
	file, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	entries, _ := file.ReadDir(4096)
	result := []uxSuggestion{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			info, err = os.Stat(filepath.Join(dir, entry.Name()))
		}
		if err != nil || directoriesOnly && !info.IsDir() {
			continue
		}
		value, description := displayDir+entry.Name(), "File"
		if info.IsDir() {
			value, description = value+string(filepath.Separator), "Directory"
		}
		result = append(result, uxSuggestion{value, description})
	}
	return result
}

func uxSuggestionValues(values []string, description string) []uxSuggestion {
	result := make([]uxSuggestion, 0, len(values))
	for _, value := range values {
		result = append(result, uxSuggestion{value, description})
	}
	return result
}

func uxCompletionRead(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("completion metadata must be a regular file of at most 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, 1<<20))
}
