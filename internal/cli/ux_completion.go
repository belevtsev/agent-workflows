package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

type uxSuggestion struct {
	Value       string `json:"value"`
	Description string `json:"description"`
}

type uxCompleteRequest struct {
	shell, format, line, word  string
	lineSet, wordSet, wordsSet bool
	words                      []string
}

type uxCompleteState struct {
	command uxCommand
	current string
	pos     []string
	values  map[string][]string
	pending *uxFlag
	literal bool
}

// runUXComplete is deliberately quiet on malformed input or local state. Shell
// queries read metadata and configuration; they never invoke a provider or skill.
func runUXComplete(ctx context.Context, cfg Config, args []string) error {
	request, ok := uxParseCompleteRequest(args)
	if !ok {
		return nil
	}
	if request.lineSet {
		request.words = uxCompletionWords(request.line, request.shell)
	}
	if len(request.words) == 0 {
		request.words = []string{""}
	}
	for _, candidate := range uxCompleteCandidates(ctx, request.words) {
		if strings.ContainsFunc(candidate.Value, unicode.IsControl) {
			continue
		}
		candidate.Description = strings.Join(strings.FieldsFunc(candidate.Description, unicode.IsControl), " ")
		if request.shell == "bash" && request.wordSet {
			prefix := request.words[len(request.words)-1]
			if strings.HasSuffix(prefix, request.word) {
				candidate.Value = strings.TrimPrefix(candidate.Value, strings.TrimSuffix(prefix, request.word))
			}
		}
		switch request.format {
		case "lines":
			fmt.Fprintln(cfg.Out, candidate.Value)
		case "tsv":
			fmt.Fprintf(cfg.Out, "%s\t%s\n", candidate.Value, candidate.Description)
		case "json-base64":
			data, err := json.Marshal(candidate)
			if err != nil {
				return err
			}
			fmt.Fprintln(cfg.Out, base64.StdEncoding.EncodeToString(data))
		default:
			if err := json.NewEncoder(cfg.Out).Encode(candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func uxParseCompleteRequest(args []string) (uxCompleteRequest, bool) {
	request := uxCompleteRequest{format: "json"}
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			request.words = args[i+1:]
			return request, true
		}
		key, value, equal := strings.Cut(args[i], "=")
		if !equal {
			if i+1 == len(args) {
				return request, false
			}
			i++
			value = args[i]
		}
		switch key {
		case "--shell":
			if !slices.Contains([]string{"bash", "zsh", "fish", "powershell"}, value) {
				return request, false
			}
			request.shell = value
		case "--format":
			if !slices.Contains([]string{"json", "json-base64", "lines", "tsv"}, value) {
				return request, false
			}
			request.format = value
		case "--line":
			request.line, request.lineSet = value, true
		case "--word":
			request.word, request.wordSet = value, true
		case "--words-base64":
			data, err := base64.StdEncoding.DecodeString(value)
			if err != nil || len(data) > 64<<10 || json.Unmarshal(data, &request.words) != nil {
				return request, false
			}
			request.wordsSet = true
		default:
			return request, false
		}
	}
	return request, request.lineSet || request.wordsSet
}

func uxCompleteCandidates(ctx context.Context, words []string) []uxSuggestion {
	current, previous := words[len(words)-1], words[:len(words)-1]
	help := len(previous) > 0 && previous[0] == "help"
	if help {
		previous = previous[1:]
	} else if len(previous) > 1 && previous[1] == "help" {
		help = true
		previous = append([]string{previous[0]}, previous[2:]...)
	}
	command, consumed, children := uxCompletionNode(previous)
	if len(previous) == 0 {
		children = append(children, uxSuggestion{"help", "Show help for a command"}, uxSuggestion{"version", "Show version"})
		if !help {
			children = append(children, uxSuggestion{"--help", "Show help"}, uxSuggestion{"-h", "Show help"}, uxSuggestion{"--version", "Show version"})
		} else {
			children = append(children, uxSuggestion{"--all", "Include legacy commands in help"})
		}
	} else if len(children) > 0 && !help {
		children = append(children, uxSuggestion{"help", "Show nested command help"}, uxSuggestion{"--help", "Show help"}, uxSuggestion{"-h", "Show help"})
	}
	if len(children) > 0 || command == nil || help {
		return uxFilterSuggestions(children, current)
	}
	state := uxParseCompletionState(*command, previous[consumed:], current)
	if state.pending != nil {
		return uxFilterSuggestions(uxCompleteValue(ctx, state, *state.pending, current), current)
	}
	if candidates, flagPrefix := uxCompleteFlagPrefix(ctx, state); flagPrefix {
		return candidates
	}
	candidates := uxCompleteOperand(ctx, state)
	if current == "" && !state.literal {
		candidates = append(candidates, uxCompletionFlags(*command)...)
	}
	return uxFilterSuggestions(candidates, current)
}

func uxCompleteFlagPrefix(ctx context.Context, state uxCompleteState) ([]uxSuggestion, bool) {
	if state.literal || !strings.HasPrefix(state.current, "-") {
		return nil, false
	}
	if name, prefix, equal := strings.Cut(state.current, "="); equal {
		if flag, ok := uxCompletionFlag(state.command, name); ok && flag.value != "" {
			matches := uxFilterSuggestions(uxCompleteValue(ctx, state, flag, prefix), prefix)
			for i := range matches {
				matches[i].Value = name + "=" + matches[i].Value
			}
			return matches, true
		}
	}
	return uxFilterSuggestions(uxCompletionFlags(state.command), state.current), true
}

func uxCompletionNode(previous []string) (*uxCommand, int, []uxSuggestion) {
	children := []uxSuggestion{}
	var command *uxCommand
	consumed := 0
	for _, candidate := range uxCommands() {
		if candidate.hidden {
			continue
		}
		path := strings.Fields(candidate.path)
		shared := min(len(path), len(previous))
		if !slices.Equal(path[:shared], previous[:shared]) {
			continue
		}
		if len(previous) < len(path) {
			children = append(children, uxSuggestion{path[len(previous)], candidate.summary})
		} else if len(path) > consumed {
			copy := candidate
			command, consumed = &copy, len(path)
		}
	}
	return command, consumed, children
}

func uxParseCompletionState(command uxCommand, previous []string, current string) uxCompleteState {
	state := uxCompleteState{command: command, current: current, values: map[string][]string{}}
	for i := 0; i < len(previous); i++ {
		word := previous[i]
		if state.literal || !strings.HasPrefix(word, "-") || word == "-" {
			state.pos = append(state.pos, word)
			continue
		}
		if word == "--" {
			state.literal = true
			continue
		}
		name, value, equal := strings.Cut(word, "=")
		flag, found := uxCompletionFlag(command, name)
		if !found {
			continue
		}
		if flag.value == "" {
			if !equal {
				value = "true"
			}
			state.values[strings.TrimLeft(flag.name, "-")] = append(state.values[strings.TrimLeft(flag.name, "-")], value)
			continue
		}
		if !equal {
			if i+1 == len(previous) {
				state.pending = &flag
				break
			}
			i++
			value = previous[i]
		}
		key := strings.TrimLeft(flag.name, "-")
		state.values[key] = append(state.values[key], value)
	}
	return state
}

func uxCompletionFlag(command uxCommand, name string) (uxFlag, bool) {
	for _, flag := range command.flags {
		if strings.TrimLeft(name, "-") == strings.TrimLeft(flag.name, "-") {
			return flag, true
		}
	}
	return uxFlag{}, false
}

func uxCompletionFlags(command uxCommand) []uxSuggestion {
	flags := []uxSuggestion{{"--help", "Show help"}, {"-h", "Show help"}}
	for _, flag := range command.flags {
		flags = append(flags, uxSuggestion{"--" + strings.TrimLeft(flag.name, "-"), flag.description})
	}
	return flags
}

func uxFilterSuggestions(candidates []uxSuggestion, prefix string) []uxSuggestion {
	slices.SortFunc(candidates, func(a, b uxSuggestion) int { return strings.Compare(a.Value, b.Value) })
	result, seen := []uxSuggestion{}, map[string]bool{}
	for _, candidate := range candidates {
		if candidate.Value != "" && strings.HasPrefix(candidate.Value, prefix) && !seen[candidate.Value] {
			result = append(result, candidate)
			seen[candidate.Value] = true
		}
	}
	return result
}

// uxCompletionWords tolerates unfinished quotes while preserving literal
// metacharacters inside them. It evaluates no substitutions or shell programs.
func uxCompletionWords(line, shell string) []string {
	if len(line) > 64<<10 {
		return nil
	}
	words, word, started, quote := []string{}, []rune{}, false, rune(0)
	flush := func() {
		if started {
			words = append(words, string(word))
			word, started = []rune{}, false
		}
	}
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		if char == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			if uxCompletionEscapes(next, quote, shell) {
				i++
				word, started = append(word, next), true
				continue
			}
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				word = append(word, char)
			}
			continue
		}
		switch {
		case char == '\'' || char == '"':
			quote, started = char, true
		case char == ';' || char == '|' || char == '&' || char == '\n':
			words, word, started = []string{}, []rune{}, false
		case unicode.IsSpace(char):
			flush()
		default:
			word, started = append(word, char), true
		}
	}
	flush()
	if len(words) > 0 {
		words = words[1:]
	}
	if quote == 0 && len(runes) > 0 && unicode.IsSpace(runes[len(runes)-1]) {
		words = append(words, "")
	}
	return words
}

func uxCompletionEscapes(next, quote rune, shell string) bool {
	if quote == '\'' {
		return shell == "fish" && (next == '\'' || next == '\\')
	}
	return quote == 0 || strings.ContainsRune("\"\\$`\n", next)
}
