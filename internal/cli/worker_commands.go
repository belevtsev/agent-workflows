package cli

func workerCommands() []uxCommand {
	json := uxOption("json", "", "print JSON; these explicit compact helpers always use JSON")
	budget := uxOption("max-bytes", "bytes", "maximum response bytes; default 16384, range 512 to 1048576")
	return []uxCommand{
		{path: "task prepare", args: "<task>", summary: "Save a worker assignment and start one private write-check interval", flags: []uxFlag{
			uxOption("host", "codex|claude", "required host", "codex", "claude"),
			uxOption("role", "role", "required specialist role"),
			uxOption("stage", "stage", "required explicit stage", "lookup", "spec", "spec-review", "design", "design-review", "tests", "code", "review", "commit"),
			uxOption("round", "number", "required positive worker round"),
			uxOption("workflow", "full|small", "default full requires a task folder; small needs explicit stage and round", "full", "small"),
			uxOption("assignment", "file|-", "required UTF-8 assignment; - reads standard input"),
			uxOption("dispatch", "", "emit version 2 with a generated fresh-worker native prompt and host settings"), budget, json,
		}, examples: []string{"kitsu task prepare fix-login --role developer --stage code --round 1 --host codex --workflow small --assignment assignment.txt"}, run: runTaskPrepare, host: true, requiredHost: true},
		{path: "task return", args: "<dispatch-id>", summary: "Bind bounded untrusted worker claims to the active interval without finishing it", operands: [][2]string{{"dispatch-id", "private interval id returned by task prepare in this worktree"}}, flags: []uxFlag{
			uxOption("request", "file|-", "required version 1 JSON handback, at most 65536 bytes; - reads standard input"), budget, json,
		}, examples: []string{"kitsu task return <dispatch-id> --request -"}, run: runTaskReturn},
		{path: "task finish", args: "<dispatch-id>", summary: "Validate a prepared role's writes and record current checks without rerunning them", operands: [][2]string{{"dispatch-id", "private interval id returned by task prepare in this worktree"}}, flags: []uxFlag{
			uxOption("expect-checks", "pass|fail|any", "default any records the actual verdict; pass requires current passes; fail requires current failures", "pass", "fail", "any"),
			uxOption("abandon", "", "record abandonment and release the interval; gives no write or quality approval"),
			uxOption("reason", "text", "required explanation with --abandon; cannot combine abandonment with --expect-checks"), budget, json,
		}, examples: []string{"kitsu task finish <dispatch-id> --expect-checks pass", "kitsu task finish <dispatch-id> --abandon --reason 'Role settings changed; prepare a fresh worker'"}, run: runTaskFinish},
	}
}
