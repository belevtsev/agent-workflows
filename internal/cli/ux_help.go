package cli

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

func uxHelp(cfg Config, path []string, all bool) error {
	var names []string
	for _, name := range path {
		if name == "--all" || name == "--help" || name == "-h" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return uxPrintRootHelp(cfg, all)
	}
	if len(names) == 1 {
		if names[0] == "__complete" {
			return uxCompletionHelp(cfg)
		}
		if uxPrintGroupHelp(cfg, names[0]) {
			return nil
		}
		for _, legacy := range commands() {
			if legacy.name == names[0] {
				return helpFor(cfg, names[0])
			}
		}
		return uxUnknown(names[0], "", uxGroupNames())
	}
	requestedNames := slices.Clone(names)
	names = uxAliasPath(names)
	for depth := len(names); depth >= 2; depth-- {
		key := strings.Join(names[:depth], " ")
		requested := strings.Join(requestedNames[:depth], " ")
		for _, c := range uxCommands() {
			if c.path != key {
				continue
			}
			if requested != key {
				c = uxAliasHelp(c, requested)
			}
			return uxPrintLeaf(cfg, c)
		}
		if depth == len(names) && uxPrintNestedHelp(cfg, key) {
			return nil
		}
	}
	parent := strings.Join(names[:len(names)-1], " ")
	return uxUnknown(names[len(names)-1], parent, uxChildren(parent))
}

func uxPrintNestedHelp(cfg Config, name string) bool {
	children := uxChildren(name)
	if len(children) == 0 {
		return false
	}
	fmt.Fprintf(cfg.Out, "usage: kitsu %s <command> [arguments]\n\nCommands:\n", name)
	for _, child := range children {
		for _, command := range uxCommands() {
			if command.path == name+" "+child {
				fmt.Fprintf(cfg.Out, "  %-12s %s\n", child, command.summary)
				break
			}
		}
	}
	fmt.Fprintf(cfg.Out, "\nRun `kitsu %s <command> --help` for arguments and examples.\n", name)
	return true
}

func uxUsage(c uxCommand) string {
	usage := strings.TrimSpace("kitsu " + c.path + " " + c.args)
	if len(c.flags) > 0 {
		usage += " [options]"
	}
	return usage
}

var uxOperand = regexp.MustCompile(`<([a-z-]+)>`)

func uxPrintLeaf(cfg Config, c uxCommand) error {
	fmt.Fprintf(cfg.Out, "usage: %s\n\n%s.\n", uxUsage(c), c.summary)
	if c.replacement != "" {
		fmt.Fprintf(cfg.Out, "\nPreferred replacement: kitsu %s. Legacy argument rules below still apply.\n", c.replacement)
	}
	var seen []string
	descriptions := map[string]string{"role": "specialist role id, or coordinator where supported", "task": "task name: lowercase words joined by hyphens", "skill": "catalog id, unique skill name, or original absolute path", "adviser": "name of an explicitly configured experimental adviser", "id": "check or consultation id, as appropriate", "tree": "starting Git tree or revision", "key": "CLI preference key; currently host", "value": "preference value; host accepts codex or claude", "program": "executable run directly without a shell", "argument": "argument passed unchanged to the check program", "text": "explicit explanation of this recorded decision", "workflow": "full for an existing task folder, or small for lightweight history", "stage": "stage of the role making this request", "file": "JSON request file, or - to read standard input", "disposition": "accept, reject or defer the consultation, with an explicit reason"}
	descriptions["action"] = "next judgment action returned by task next"
	descriptions["host"] = "codex or claude; legacy aliases require explicit host selection"
	descriptions["word"] = "literal command word; the final word is the current prefix, including an empty prefix"
	for _, match := range uxOperand.FindAllStringSubmatch(c.args, -1) {
		name := match[1]
		if slices.Contains(seen, name) {
			continue
		}
		if len(seen) == 0 {
			fmt.Fprintln(cfg.Out, "\nArguments:")
		}
		text := descriptions[name]
		for _, operand := range c.operands {
			if operand[0] == name {
				text = operand[1]
			}
		}
		fmt.Fprintf(cfg.Out, "  %-18s %s\n", "<"+name+">", text)
		seen = append(seen, name)
	}
	fmt.Fprintln(cfg.Out, "\nOptions:")
	for _, flag := range append(slices.Clone(c.flags), uxOption("help, -h", "", "show this command's help without executing it")) {
		label := "--" + flag.name
		if flag.value != "" {
			label += " <" + flag.value + ">"
		}
		uxHelpColumn(cfg, label, flag.description, 24)
	}
	if c.host {
		fmt.Fprintln(cfg.Out, "\nHost: --host > KITSU_HOST > private checkout preference > personal preference.\nSet a preference with `kitsu config set host codex` (or add --global).")
	}
	if c.path == "agent check" || c.path == "task start" {
		fmt.Fprintln(cfg.Out, "\nSetup: local inspection does not prove host authentication, model access, hook\ntrust or runtime execution. Follow the reported manual host checks.")
	}
	if c.path == "task start" {
		fmt.Fprintln(cfg.Out, "\nStart: requires terminal input, stdout and stderr and no known local blockers.\nDaily startup pins a task before the host opens in a repository. Use --resume\nto reopen it. The host may request permissions. --print and\n--json prepare a handoff even when setup is incomplete; they execute nothing\nand create no task records. The two output options are mutually exclusive.\nA launched host's exit status is preserved; interruption exits 130.")
	}
	if strings.HasPrefix(c.path, "skill ") || strings.HasPrefix(c.path, "role ") {
		fmt.Fprintln(cfg.Out, "\nRole settings: repository skills replace personal lists; clear pins none; reset\nrestores inheritance. Personal settings and coordinator accept only skills.\nSelections grant no extra permissions and do not prove host loading.")
	}
	if strings.HasSuffix(c.path, " edit") {
		fmt.Fprintln(cfg.Out, "\nEditor: F1 opens field help; Save applies the preview; Cancel writes nothing.\nCtrl+C exits 130 without saving. Requires a terminal; scripts use set with --json.")
	}
	if c.path == "adviser ask" {
		fmt.Fprintln(cfg.Out, "\nRequest: CLI advisers accept version 1 advice/decision JSON; native advisers\naccept decision, score and boolean requests (version 2 for score/boolean).\nThe call may execute a configured CLI or contact an enabled experimental API.\nThe complete exchange is recorded. Answers provide no automatic approval.")
	}
	if c.path == "gate uninstall" {
		fmt.Fprintln(cfg.Out, "\nRemoval: records are retained. --purge deletes shared check and token records\nonly after all Kitsu hook calls are detached. Task history and configuration stay.")
	}
	fmt.Fprintln(cfg.Out, "\nExamples:")
	for _, example := range c.examples {
		fmt.Fprintln(cfg.Out, "  "+example)
	}
	fmt.Fprintln(cfg.Out, "\nExit statuses: 0 success; 1 operation failed; 2 invalid arguments;\n3 gate denial; 4 foreign hook needs attention; 5 not verified.")
	return nil
}

func uxPrintRootHelp(cfg Config, all bool) error {
	fmt.Fprintln(cfg.Out, "Kitsu coordinates coding agents through roles, skills and task records.\n\nusage: kitsu <group> <command> [arguments]\n\nCommands:")
	for _, group := range uxGroups {
		fmt.Fprintf(cfg.Out, "  %-12s %s\n", group[0], group[1])
	}
	fmt.Fprintln(cfg.Out, "  version      Print build identity\n\nExamples:\n  kitsu agent check --host codex\n  kitsu task start \"Describe the change\" --host codex\n  kitsu config set host codex --global\n\nHelp: kitsu <group> --help; kitsu help <group> <command>\nUse `kitsu help --all` for compatibility aliases and agent integrations.")
	if all {
		fmt.Fprintln(cfg.Out, "\nCompatibility aliases and advanced integrations:")
		for _, c := range commands() {
			fmt.Fprintf(cfg.Out, "  %-12s %s\n", c.name, c.summary)
		}
		fmt.Fprintln(cfg.Out, "  __complete   Internal read-only shell completion callback")
	}
	return nil
}

func uxPrintGroupHelp(cfg Config, name string) bool {
	for _, group := range uxGroups {
		if name != group[0] {
			continue
		}
		if name == "context" {
			_ = helpFor(cfg, name)
			return true
		}
		fmt.Fprintf(cfg.Out, "usage: kitsu %s <command> [arguments]\n\n%s.\n\nCommands:\n", group[0], group[1])
		for _, c := range uxCommands() {
			if strings.HasPrefix(c.path, group[0]+" ") && !c.hidden {
				fmt.Fprintf(cfg.Out, "  %-12s %s\n", strings.TrimPrefix(c.path, group[0]+" "), c.summary)
			}
		}
		fmt.Fprintf(cfg.Out, "\nRun `kitsu %s <command> --help` for arguments and examples.\n", group[0])
		return true
	}
	return false
}

// Keep option descriptions readable while leaving command examples copyable.
func uxHelpColumn(cfg Config, label, description string, width int) {
	prefix := fmt.Sprintf("  %-*s ", width, label)
	line, indent := prefix, strings.Repeat(" ", len(prefix))
	for _, word := range strings.Fields(description) {
		if len([]rune(line+word)) > 80 && line != prefix && line != indent {
			fmt.Fprintln(cfg.Out, strings.TrimRight(line, " "))
			line = indent
		}
		line += word + " "
	}
	fmt.Fprintln(cfg.Out, strings.TrimRight(line, " "))
}
