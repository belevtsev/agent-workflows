package cli

import (
	"fmt"
	"strings"
)

func alignedHelp(rows ...[2]string) string {
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	var b strings.Builder
	b.WriteByte('\n')
	for _, row := range rows {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, row[0], row[1])
	}
	return b.String()
}

func adviserHelp() string {
	return alignedHelp(
		[2]string{"providers", "list built-in native presets and credentials; offline, works outside a repository"},
		[2]string{"list", "print configured advisers; --task selects the saved binding"},
		[2]string{"bind", "snapshot configuration with --task and --workflow"},
		[2]string{"resolve <id>", "append a disposition to a consultation"},
		[2]string{"status|check", "inspect readiness or require satisfied checkpoints; never invoke models"},
		[2]string{"--task <task>", "task name; required except with providers or list"},
		[2]string{"--role <role>", "with list: show only advisers this requester may consult"},
		[2]string{"--workflow full|small", "with bind: use a change folder or lightweight task record"},
		[2]string{"--refresh", "with bind: explicitly update the saved policy or promote small to full"},
		[2]string{"--disposition <value>", "with resolve: accept, reject or defer"},
		[2]string{"--reason <text>", "with resolve: required explanation of the disposition"},
		[2]string{"--choice <id>", "with resolve: optional selected option from a decision request"},
		[2]string{"--checkpoint <id>", "with status/check: consider this required checkpoint only"},
		[2]string{"--json", "print one JSON object"},
	)
}

func consultHelp() string {
	return alignedHelp(
		[2]string{"--task <task>", "task with a saved adviser binding"},
		[2]string{"--role <role>", "original requester, including coordinator"},
		[2]string{"--stage <stage>", "workflow stage of this request"},
		[2]string{"--adviser <name>", "allowed adviser whose definition still matches the binding"},
		[2]string{"--request <file|->", "version 1 or 2 JSON file, or standard input; one question"},
		[2]string{"--checkpoint <id>", "required checkpoint this consultation addresses"},
		[2]string{"--json", "print the saved consultation, including its ID and outcome"},
		[2]string{"Advice JSON", `{"version":1,"kind":"advice","question":"What could fail?","context":["src/app.txt"]}`},
		[2]string{"Decision JSON", `set kind to decision and add choices: [{"id":"a","text":"First option"},{"id":"b","text":"Second option"}]`},
		[2]string{"Score JSON", `{"version":2,"kind":"score","question":"How complete is the design?","levels":["Incomplete","Partial","Complete"]}`},
		[2]string{"Boolean JSON", `{"version":2,"kind":"boolean","question":"Does the design handle cancellation?","criteria":{"true":"Explicit cancellation","false":"No cancellation"}}`},
		[2]string{"Capabilities", "CLI: advice, decision. Native: decision, score, boolean. Results never automatically approve work."},
	)
}

func roleTableHelp() string {
	return alignedHelp(
		[2]string{"--host claude|codex", "select a host; required with set and reset"},
		[2]string{"--json", "print the roles as one JSON object"},
		[2]string{"set <role>", "save model and/or reasoning overrides in the repository's kitsu-roles.toml"},
		[2]string{"--model <model>", "with set: change only this role's model for the selected host"},
		[2]string{"--reasoning <effort>", "with set: change reasoning effort; omit either option to keep its current setting"},
		[2]string{"reset <role>", "remove both overrides for this role and host; restore built-in defaults"},
		[2]string{"--task <task>", "with the table: read bound adviser access; with check: set the task write scope"},
		[2]string{"check <role> --from <tree>", "exit 5 when writes since this tree exceed the role's scope"},
		[2]string{"--finalize", "with spec-writer and --task: allow current specs for capabilities in --from"},
		[2]string{"text <role>", "print one role's instructions, or coordinator"},
	)
}
