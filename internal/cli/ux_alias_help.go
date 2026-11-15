package cli

import "strings"

// Alias help preserves legacy argument rules instead of teaching the preferred
// path's defaults or positional operands to callers of a different protocol.
func uxAliasHelp(command uxCommand, requested string) uxCommand {
	command.path, command.replacement, command.host = requested, command.path, false
	for i := range command.examples {
		command.examples[i] = strings.Replace(command.examples[i], "kitsu "+command.replacement, "kitsu "+requested, 1)
	}
	for i := range command.flags {
		if command.flags[i].name == "host" {
			command.flags[i].description = "required explicit legacy host; saved preferences apply to preferred commands"
		}
	}
	if strings.HasPrefix(requested, "roles ") || strings.HasPrefix(requested, "skills ") {
		if command.requiredHost {
			command.args += " --host <host>"
		}
	}
	taskFlag := uxOption("task", "task", "required task name supplied with --task")
	switch requested {
	case "advisers bind":
		command.args = "--task <task> --workflow <workflow>"
		command.flags = append(command.flags, taskFlag)
		command.examples = []string{"kitsu advisers bind --task add-login --workflow full"}
	case "advisers status", "advisers check":
		command.args = "--task <task>"
		command.flags = append(command.flags, taskFlag)
		command.examples = []string{"kitsu " + requested + " --task add-login"}
	case "advisers resolve":
		command.examples = []string{"kitsu advisers resolve <consultation-id> --task add-login --disposition accept --reason 'Reviewed the answer'"}
	case "change status":
		command.args = "[<task>]"
		command.summary = "Print one full task's stage or list all full task folders"
		command.examples = []string{"kitsu change status", "kitsu change status add-login --json"}
	case "tokens record":
		command.flags[0].description = "notice format; legacy default is claude; specify codex explicitly for Codex"
	}
	return command
}

func uxCompletionHelp(cfg Config) error {
	return uxPrintLeaf(cfg, uxCommand{
		path: "__complete", args: "-- <word>...", summary: "Read-only internal completion callback; use completion commands for shell setup",
		flags: []uxFlag{
			uxOption("shell", "shell", "bash, zsh, fish or powershell"),
			uxOption("format", "format", "json, json-base64, lines or tsv; default json"),
			uxOption("line", "text", "raw shell prefix to decode without evaluation"),
			uxOption("word", "text", "current Bash word segment for word-break handling"),
			uxOption("words-base64", "data", "base64 JSON literal word array, preserving empty native arguments"),
		},
		examples: []string{"kitsu completion zsh", "kitsu __complete --shell bash -- role ''"},
	})
}
