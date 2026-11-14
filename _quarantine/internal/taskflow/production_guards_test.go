package taskflow

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func productionAdmission(test *testing.T, argv []string, allowed bool) {
	test.Helper()
	before := slices.Clone(argv)
	err := validateCommand(argv)
	if (err == nil) != allowed {
		test.Fatalf("admission(%q) = %v, allowed = %t", argv, err, allowed)
	}
	if !reflect.DeepEqual(argv, before) {
		test.Fatalf("admission mutated literal argv: got %q, want %q", argv, before)
	}
}

func TestProductionGitDispatchAndGlobalArity(test *testing.T) {
	for _, command := range []string{"diff", "status", "log", "show", "rev-parse", "rev-list", "ls-files", "ls-tree", "merge-base", "describe"} {
		test.Run("allowed/"+command, func(test *testing.T) {
			productionAdmission(test, []string{"git", command}, true)
			productionAdmission(test, []string{"git", "--", command, "", "push", "apply", "$(literal)", ";literal"}, true)
		})
	}
	for _, command := range []string{"push", "apply", "fetch", "pull", "clone", "add", "commit", "config", "help", "version", "archive", "checkout", "reset", "rebase", "tag", "branch", "unknown", "Status", ""} {
		test.Run("denied/"+command, func(test *testing.T) {
			productionAdmission(test, []string{"git", command}, false)
		})
	}
	for _, option := range []string{"-C", "--git-dir", "--work-tree", "--namespace", "--super-prefix"} {
		test.Run("arity/"+option, func(test *testing.T) {
			productionAdmission(test, []string{"git", option, "push", "status", "--short"}, true)
			productionAdmission(test, []string{"git", option, "apply", "diff", "--", "push"}, true)
			joined := option + "=path with spaces"
			if option == "-C" {
				joined = "-Cpath with spaces"
			}
			productionAdmission(test, []string{"git", joined, "status"}, true)
			productionAdmission(test, []string{"git", option}, false)
			productionAdmission(test, []string{"git", option, "status"}, false)
		})
	}
	for _, option := range []string{"-P", "--no-pager", "--bare", "--no-replace-objects", "--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs", "--no-optional-locks", "--no-lazy-fetch", "--no-advice"} {
		test.Run("flag/"+option, func(test *testing.T) {
			productionAdmission(test, []string{"git", option, "status"}, true)
		})
	}
	for _, option := range []string{"-v", "--version", "--html-path", "--man-path", "--info-path", "--exec-path"} {
		test.Run("output/"+option, func(test *testing.T) {
			productionAdmission(test, []string{"git", option}, true)
			productionAdmission(test, []string{"git", "-C", "push", "-P", option}, true)
			productionAdmission(test, []string{"git", option, "status"}, false)
			productionAdmission(test, []string{"git", "--", option}, false)
		})
	}
	for _, option := range []string{"--exec-path=/tmp/production-executor", "--exec-path=", "-p", "--paginate", "--list-cmds", "--list-cmds=main", "--list-cmds=", "--unknown", "-z", "--config", "--help"} {
		test.Run("unsafe-global/"+option, func(test *testing.T) {
			productionAdmission(test, []string{"git", option, "diff"}, false)
		})
	}
	productionAdmission(test, nil, false)
	productionAdmission(test, []string{"git"}, false)
	productionAdmission(test, []string{"git", "--"}, false)
	productionAdmission(test, []string{"git", "diff", "-p", "-c", "alias.anything=!not executed", "--config-env=core.pager=ANYTHING", "", "push"}, true)
	productionAdmission(test, []string{filepath.Join("tool directory", "GiT.ExE"), "status"}, true)
}

func TestProductionGitConfigPayloads(test *testing.T) {
	forms := func(payload string) [][]string {
		return [][]string{
			{"git", "-c", payload, "status"},
			{"git", "-c" + payload, "status"},
		}
	}
	environmentForms := func(payload string) [][]string {
		return [][]string{
			{"git", "--config-env", payload, "status"},
			{"git", "--config-env=" + payload, "status"},
		}
	}
	for _, key := range []string{"core.quotePath", "color.ui", "diff.noprefix", "diff.mnemonicPrefix", "diff.context", "status.short", "status.branch"} {
		test.Run("allowed/"+key, func(test *testing.T) {
			for _, spelling := range []string{key, strings.ToUpper(key)} {
				for _, payload := range []string{spelling, spelling + "=0", spelling + "=", spelling + "=literal=argument"} {
					for _, argv := range forms(payload) {
						productionAdmission(test, argv, true)
					}
				}
				for _, argv := range environmentForms(spelling + "=CW_PRODUCTION_CONFIG_VALUE") {
					productionAdmission(test, argv, true)
				}
			}
		})
	}
	for _, key := range []string{"alias.inspect", "alias.status", "unknown.key", "core.pager", "pager.log", "core.sshCommand", "core.editor", "core.hooksPath", "diff.external", "diff.fixture.command", "include.path", "includeIf.onbranch:main.path", "interactive.diffFilter", " core.quotePath", "core.quotePath ", "core.quotepath.extra"} {
		test.Run("denied/"+key, func(test *testing.T) {
			for _, argv := range forms(key + "=!literal executor payload") {
				productionAdmission(test, argv, false)
			}
			for _, argv := range environmentForms(key + "=CW_PRODUCTION_CONFIG_VALUE") {
				productionAdmission(test, argv, false)
			}
		})
	}
	for _, payload := range []string{"", "=value", " core.quotePath=false", "core.quotePath =false"} {
		for _, argv := range forms(payload) {
			productionAdmission(test, argv, false)
		}
	}
	for _, payload := range []string{"", "core.quotePath", "=ENV", "core.quotePath=", "core.quotePath=ENV=EXTRA"} {
		for _, argv := range environmentForms(payload) {
			productionAdmission(test, argv, false)
		}
	}
	productionAdmission(test, []string{"git", "-c"}, false)
	productionAdmission(test, []string{"git", "--config-env"}, false)
	for _, value := range []string{"", "!not-an-execution-oracle; $(literal)"} {
		test.Setenv("CW_PRODUCTION_CONFIG_VALUE", value)
		productionAdmission(test, []string{"git", "--config-env=core.quotePath=CW_PRODUCTION_CONFIG_VALUE", "status"}, true)
		productionAdmission(test, []string{"git", "--config-env=alias.inspect=CW_PRODUCTION_CONFIG_VALUE", "status"}, false)
	}
}

func TestProductionOtherDispatchRemainsLiteral(test *testing.T) {
	for _, tool := range []string{"terraform", "tofu", filepath.Join("tool directory", "TERRAFORM.EXE"), filepath.Join("tool directory", "ToFu.ExE")} {
		for _, argv := range [][]string{
			{tool, "fmt", "-check"}, {tool, "validate"}, {tool, "test"}, {tool, "plan", "apply"},
			{tool, "-chdir", "apply", "fmt", "-check"}, {tool, "-chdir=path with spaces", "validate"},
			{tool, "-chdir", "validate"},
			{tool, "--", "fmt", "destroy"}, {tool, "-version"}, {tool, "--version"}, {tool, "-help"}, {tool, "--help"},
		} {
			productionAdmission(test, argv, true)
		}
		for _, argv := range [][]string{
			{tool, "apply"}, {tool, "destroy"}, {tool, "-chdir", "path with spaces", "apply"},
			{tool, "-chdir=path with spaces", "destroy"}, {tool, "--", "apply"}, {tool, "-chdir"},
			{tool, "--unknown", "validate"},
		} {
			productionAdmission(test, argv, false)
		}
	}
	for _, argv := range [][]string{{"rojo", "build", "file with spaces.project.json"}, {"lune", "run", "validate"}, {"go", "test", "-run", "deploy"}} {
		productionAdmission(test, argv, true)
	}
	for _, argv := range [][]string{{"sh", "-c", "true"}, {"npm", "publish"}} {
		productionAdmission(test, argv, false)
	}
}

func productionBasePlan() Plan {
	return Plan{
		Title:         "Independent contract fixture",
		Kind:          "small",
		CoordinatorID: "production-test-author",
		Requirements:  []Requirement{{ID: "behavior", Description: "Contract behavior", CheckIDs: []string{"inspection"}}},
		Checks:        []Check{{ID: "inspection", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
	}
}

func productionClonePlan(plan Plan) Plan {
	plan.Requirements = slices.Clone(plan.Requirements)
	for index := range plan.Requirements {
		plan.Requirements[index].CheckIDs = slices.Clone(plan.Requirements[index].CheckIDs)
	}
	plan.Checks = slices.Clone(plan.Checks)
	for index := range plan.Checks {
		plan.Checks[index].Argv = slices.Clone(plan.Checks[index].Argv)
	}
	return plan
}

func TestProductionCommandShapeIsRepresentationOnly(test *testing.T) {
	cases := []struct {
		Name  string
		Argv  []string
		Valid bool
	}{
		{Name: "nil"},
		{Name: "empty", Argv: []string{}},
		{Name: "blank-executable", Argv: []string{" \t", "status"}},
		{Name: "empty-executable", Argv: []string{"", "status"}},
		{Name: "nul-executable", Argv: []string{"git\x00", "status"}},
		{Name: "nul-operand", Argv: []string{"git", "a\x00b"}},
		{Name: "denied-dispatch-shape", Argv: []string{"git", "push"}, Valid: true},
		{Name: "shell-is-still-representable", Argv: []string{"sh", "-c", "not executed"}, Valid: true},
		{Name: "empty-operands", Argv: []string{"git", "diff", "", " ", "$(literal)", ";literal"}, Valid: true},
		{Name: "one-arg-byte-limit", Argv: []string{strings.Repeat("a", 65536)}, Valid: true},
		{Name: "one-arg-byte-overflow", Argv: []string{strings.Repeat("a", 65537)}},
		{Name: "combined-byte-limit", Argv: []string{"x", strings.Repeat("a", 65535)}, Valid: true},
		{Name: "combined-byte-overflow", Argv: []string{"x", strings.Repeat("a", 65536)}},
	}
	for _, count := range []int{256, 257} {
		argv := make([]string, count)
		argv[0] = "git"
		cases = append(cases, struct {
			Name  string
			Argv  []string
			Valid bool
		}{Name: fmt.Sprintf("argc-%d", count), Argv: argv, Valid: count == 256})
	}
	for _, scenario := range cases {
		test.Run(scenario.Name, func(test *testing.T) {
			before := slices.Clone(scenario.Argv)
			err := validateCommandShape(scenario.Argv)
			if (err == nil) != scenario.Valid || !reflect.DeepEqual(before, scenario.Argv) {
				test.Fatalf("shape valid=%t err=%v, mutation=%t", scenario.Valid, err, !reflect.DeepEqual(before, scenario.Argv))
			}
		})
	}
}

func productionStoredPlan(test *testing.T, plan Plan, valid bool) {
	test.Helper()
	before := productionClonePlan(plan)
	err := validateRecordedPlan(plan)
	if (err == nil) != valid {
		test.Fatalf("stored plan validation = %v, valid = %t", err, valid)
	}
	if !reflect.DeepEqual(plan, before) {
		test.Fatal("recorded validator mutated a plan or a shared slice")
	}
}

func TestProductionRecordedPlanBoundsAndPurity(test *testing.T) {
	cases := []struct {
		Name   string
		Mutate func(*Plan)
		Valid  bool
	}{
		{Name: "base", Mutate: func(plan *Plan) {}, Valid: true},
		{Name: "title-empty", Mutate: func(plan *Plan) { plan.Title = "" }},
		{Name: "title-blank", Mutate: func(plan *Plan) { plan.Title = " \n\t" }},
		{Name: "title-limit", Mutate: func(plan *Plan) { plan.Title = strings.Repeat("é", 2048) }, Valid: true},
		{Name: "title-overflow", Mutate: func(plan *Plan) { plan.Title = strings.Repeat("é", 2049) }},
		{Name: "kind-unknown", Mutate: func(plan *Plan) { plan.Kind = "other" }},
		{Name: "kind-substantial", Mutate: func(plan *Plan) { plan.Kind = "substantial" }, Valid: true},
		{Name: "coordinator-optional-structure", Mutate: func(plan *Plan) { plan.CoordinatorID = "" }, Valid: true},
		{Name: "coordinator-limit", Mutate: func(plan *Plan) { plan.CoordinatorID = strings.Repeat("é", 128) }, Valid: true},
		{Name: "coordinator-overflow", Mutate: func(plan *Plan) { plan.CoordinatorID = strings.Repeat("a", 257) }},
		{Name: "coordinator-leading-space", Mutate: func(plan *Plan) { plan.CoordinatorID = " author" }},
		{Name: "coordinator-trailing-space", Mutate: func(plan *Plan) { plan.CoordinatorID = "author " }},
		{Name: "coordinator-unicode-control", Mutate: func(plan *Plan) { plan.CoordinatorID = "auth\u0085or" }},
		{Name: "description-empty", Mutate: func(plan *Plan) { plan.Requirements[0].Description = " \n" }},
		{Name: "description-limit", Mutate: func(plan *Plan) { plan.Requirements[0].Description = strings.Repeat("é", 8192) }, Valid: true},
		{Name: "description-overflow", Mutate: func(plan *Plan) { plan.Requirements[0].Description = strings.Repeat("é", 8193) }},
		{Name: "duplicate-requirement", Mutate: func(plan *Plan) { plan.Requirements = append(plan.Requirements, plan.Requirements[0]) }},
		{Name: "duplicate-check", Mutate: func(plan *Plan) { plan.Checks = append(plan.Checks, plan.Checks[0]) }},
		{Name: "duplicate-check-ref", Mutate: func(plan *Plan) { plan.Requirements[0].CheckIDs = []string{"inspection", "inspection"} }},
		{Name: "unknown-check-ref", Mutate: func(plan *Plan) { plan.Requirements[0].CheckIDs = []string{"unknown"} }},
		{Name: "missing-execution-declaration", Mutate: func(plan *Plan) { plan.Requirements[0].CheckIDs = nil }},
		{Name: "explicit-nonexecution", Mutate: func(plan *Plan) {
			plan.Requirements[0].CheckIDs = nil
			plan.Requirements[0].AllowNonExecution = true
			plan.Checks = nil
		}, Valid: true},
		{Name: "timeout-negative", Mutate: func(plan *Plan) { plan.Checks[0].TimeoutSeconds = -1 }},
		{Name: "stored-timeout-zero", Mutate: func(plan *Plan) { plan.Checks[0].TimeoutSeconds = 0 }},
		{Name: "timeout-min", Mutate: func(plan *Plan) { plan.Checks[0].TimeoutSeconds = 1 }, Valid: true},
		{Name: "timeout-max", Mutate: func(plan *Plan) { plan.Checks[0].TimeoutSeconds = 3600 }, Valid: true},
		{Name: "timeout-overflow", Mutate: func(plan *Plan) { plan.Checks[0].TimeoutSeconds = 3601 }},
		{Name: "historically-denied-command", Mutate: func(plan *Plan) { plan.Checks[0].Argv = []string{"git", "-c", "core.pager=cat", "diff"} }, Valid: true},
		{Name: "historical-shell-representation", Mutate: func(plan *Plan) { plan.Checks[0].Argv = []string{"sh", "-c", "not executed"} }, Valid: true},
		{Name: "argv-empty", Mutate: func(plan *Plan) { plan.Checks[0].Argv = nil }},
		{Name: "argv-blank-executable", Mutate: func(plan *Plan) { plan.Checks[0].Argv = []string{" \t"} }},
		{Name: "argv-nul", Mutate: func(plan *Plan) { plan.Checks[0].Argv = []string{"git", "a\x00b"} }},
	}
	for _, scenario := range cases {
		test.Run(scenario.Name, func(test *testing.T) {
			plan := productionBasePlan()
			scenario.Mutate(&plan)
			productionStoredPlan(test, plan, scenario.Valid)
		})
	}
	for _, identifier := range []string{"", "1bad", "_bad", "with space", "a.b", "a/b", "é", strings.Repeat("a", 65)} {
		test.Run("invalid-id/"+identifier, func(test *testing.T) {
			plan := productionBasePlan()
			plan.Requirements[0].ID = identifier
			productionStoredPlan(test, plan, false)
			plan = productionBasePlan()
			plan.Checks[0].ID = identifier
			plan.Requirements[0].CheckIDs = []string{identifier}
			productionStoredPlan(test, plan, false)
		})
	}
	for _, identifier := range []string{"a", "A_9-z", "A" + strings.Repeat("_", 63)} {
		plan := productionBasePlan()
		plan.Requirements[0].ID = identifier
		plan.Checks[0].ID = identifier
		plan.Requirements[0].CheckIDs = []string{identifier}
		productionStoredPlan(test, plan, true)
	}
	for _, count := range []int{0, 1, 256, 257} {
		test.Run(fmt.Sprintf("requirements-%d", count), func(test *testing.T) {
			plan := productionBasePlan()
			plan.Requirements = make([]Requirement, count)
			for index := range plan.Requirements {
				plan.Requirements[index] = Requirement{ID: fmt.Sprintf("requirement_%d", index), Description: "Bounded requirement", CheckIDs: []string{"inspection"}}
			}
			productionStoredPlan(test, plan, count >= 1 && count <= 256)
		})
	}
	for _, count := range []int{256, 257} {
		test.Run(fmt.Sprintf("checks-%d", count), func(test *testing.T) {
			plan := productionBasePlan()
			plan.Checks = make([]Check, count)
			plan.Requirements[0].CheckIDs = nil
			for index := range plan.Checks {
				identifier := fmt.Sprintf("check_%d", index)
				plan.Checks[index] = Check{ID: identifier, Argv: []string{"git", "status"}, TimeoutSeconds: 1}
				plan.Requirements[0].CheckIDs = append(plan.Requirements[0].CheckIDs, identifier)
			}
			productionStoredPlan(test, plan, count == 256)
		})
	}
}

func TestProductionNewPlanDefaultsAndStrictAdmission(test *testing.T) {
	plan := productionBasePlan()
	plan.Checks[0].TimeoutSeconds = 0
	plan.Checks[0].Argv = []string{"git", "-c", "core.quotePath=false", "diff", "--", "push", ""}
	expected := productionClonePlan(plan)
	expected.Checks[0].TimeoutSeconds = 300
	if err := ValidatePlan(&plan); err != nil {
		test.Fatal(err)
	}
	if !reflect.DeepEqual(plan, expected) {
		test.Fatal("new validation changed fields other than the specified timeout default")
	}
	for _, argv := range [][]string{{"git", "push"}, {"git", "-c", "core.pager=cat", "diff"}, {"git", "--config-env=alias.inspect=CW_PRODUCTION_ALIAS", "inspect"}} {
		plan := productionBasePlan()
		plan.Checks[0].Argv = argv
		productionStoredPlan(test, plan, true)
		if err := ValidatePlan(&plan); err == nil {
			test.Fatalf("new plan admitted historical unsafe argv: %q", argv)
		}
	}
}

func productionTaskRead(test *testing.T, path string) []byte {
	test.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	return data
}

func productionTaskWrite(test *testing.T, path string, data []byte) {
	test.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		test.Fatal(err)
	}
}

func productionTaskBytes(test *testing.T, directory string) map[string]string {
	test.Helper()
	result := make(map[string]string)
	if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[relative+string(filepath.Separator)] = "directory"
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected nonregular fixture artifact: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[relative] = string(data)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	return result
}

func productionRewriteHistory(test *testing.T, record Record, mutate func(int, *Record)) {
	test.Helper()
	previous := ""
	for revision := 1; revision <= record.Revision; revision++ {
		path := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", revision))
		var historical Record
		if err := Decode(bytes.NewReader(productionTaskRead(test, path)), &historical); err != nil {
			test.Fatal(err)
		}
		mutate(revision, &historical)
		historical.PreviousSHA256 = previous
		data, err := encode(historical)
		if err != nil {
			test.Fatal(err)
		}
		if revision%2 == 0 {
			data = append(append([]byte(" \n"), data...), '\n')
		}
		productionTaskWrite(test, path, data)
		previous = digest(data)
	}
}

func productionHistoricalFixture(test *testing.T, denied []string) Record {
	test.Helper()
	plan := helperPlan(test, "small", "pass", "path with spaces", "$(literal)", ";literal", "", "push")
	plan.CoordinatorID = "production-test-author"
	plan.Checks = append(plan.Checks, Check{ID: "historical", Argv: slices.Clone(plan.Checks[0].Argv), TimeoutSeconds: 10})
	plan.Requirements[0].CheckIDs = append(plan.Requirements[0].CheckIDs, "historical")
	record, _ := fixtureTask(test, plan)
	run, err := RunCheck(test.Context(), record.Directory, "historical")
	if err != nil || run.ExitCode == nil || *run.ExitCode != 0 {
		test.Fatalf("cannot seed completed harmless fixture: %#v %v", run, err)
	}
	record, err = Load(record.Directory)
	if err != nil {
		test.Fatal(err)
	}
	productionRewriteHistory(test, record, func(revision int, historical *Record) {
		historical.Plan.Checks[1].Argv = slices.Clone(denied)
		for index := range historical.Checks {
			if historical.Checks[index].CheckID == "historical" {
				historical.Checks[index].Argv = slices.Clone(denied)
			}
		}
	})
	return record
}

func TestProductionHistoricalDeniedPlansStayReadable(test *testing.T) {
	for _, payload := range []string{"pager", "alias-c", "alias-config-env"} {
		test.Run(fmt.Sprintf("v%d/%s", Version, payload), func(test *testing.T) {
			marker := filepath.Join(test.TempDir(), "must-not-execute")
			shellPayload := "!printf executed > '" + strings.ReplaceAll(marker, "'", "'\\''") + "'"
			denied := []string{"git", "-c", "core.pager=cat", "diff"}
			if payload == "alias-c" {
				denied = []string{"git", "-calias.production=" + shellPayload, "production"}
			} else if payload == "alias-config-env" {
				test.Setenv("CW_PRODUCTION_ALIAS", shellPayload)
				denied = []string{"git", "--config-env=alias.production=CW_PRODUCTION_ALIAS", "production"}
			}
			record := productionHistoricalFixture(test, denied)
			original := productionTaskBytes(test, record.Directory)
			loaded, err := Load(record.Directory)
			if err != nil || loaded.Version != Version || !slices.Equal(loaded.Plan.Checks[1].Argv, denied) {
				test.Fatalf("historical plan is not structurally readable: %#v %v", loaded.Plan, err)
			}
			terminalFound := false
			for _, run := range loaded.Checks {
				if run.CheckID == "historical" && run.FinishedAt != nil {
					terminalFound = true
					if !slices.Equal(run.Argv, denied) {
						test.Fatal("terminal recorded argv was normalized or rewritten")
					}
				}
			}
			if !terminalFound || !reflect.DeepEqual(productionTaskBytes(test, record.Directory), original) {
				test.Fatal("load mutated history or omitted terminal fixture")
			}
			if _, err := RunCheck(test.Context(), record.Directory, "historical"); err == nil {
				test.Fatal("historical readability authorized denied execution")
			}
			if !reflect.DeepEqual(productionTaskBytes(test, record.Directory), original) {
				test.Fatal("denied execution changed log/revision bytes or created artifacts")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				test.Fatalf("historical read/run executed payload: %v", err)
			}
			prepared, err := PrepareContext(test.Context(), record.Directory, "before")
			if err != nil || prepared.TaskID != loaded.ID || prepared.Phase != "before" {
				test.Fatalf("safe metadata update denied: %#v %v", prepared, err)
			}
			run, err := RunCheck(test.Context(), record.Directory, "focused")
			expectedOutput := "path with spaces|$(literal)|;literal||push"
			if err != nil || run.ExitCode == nil || *run.ExitCode != 0 || run.OutputSHA256 != digest([]byte(expectedOutput)) || run.OutputBytes != int64(len(expectedOutput)) || run.Truncated {
				test.Fatalf("allowed sibling did not preserve literal output: %#v %v", run, err)
			}
			updated, err := Load(record.Directory)
			if err != nil || updated.Version != Version || !slices.Equal(updated.Plan.Checks[1].Argv, denied) || !slices.Equal(run.Argv, updated.Plan.Checks[0].Argv) {
				test.Fatalf("metadata/sibling update changed historical plan: %v", err)
			}
			for name, data := range original {
				if strings.HasPrefix(filepath.Base(name), "record-") && string(productionTaskRead(test, filepath.Join(record.Directory, name))) != data {
					test.Fatalf("ordinary update rewrote original revision %s", name)
				}
			}
		})
	}
}

func TestProductionStartRejectsDeniedBeforeArtifacts(test *testing.T) {
	for _, argv := range [][]string{
		{"git", "-c", "core.pager=cat", "diff"},
		{"git", "-calias.production=!not executed", "production"},
		{"git", "--config-env=alias.production=CW_PRODUCTION_ALIAS", "production"},
	} {
		test.Run(strings.Join(argv[1:], "_"), func(test *testing.T) {
			repository := test.TempDir()
			productionTaskWrite(test, filepath.Join(repository, "source.txt"), []byte("synthetic source\n"))
			store := filepath.Join(test.TempDir(), "tasks")
			if err := os.Mkdir(store, 0o700); err != nil {
				test.Fatal(err)
			}
			plan := productionBasePlan()
			plan.Checks[0].Argv = slices.Clone(argv)
			if _, err := StartWithPacks(test.Context(), repository, store, plan, fixtureBinding(), SelectionExplicit, nil); err == nil {
				test.Fatal("new Start admitted denied inspection command")
			}
			if err := filepath.WalkDir(store, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() && !strings.Contains(strings.ToLower(entry.Name()), "lock") {
					return fmt.Errorf("denied Start published an artifact: %s", path)
				}
				return nil
			}); err != nil {
				test.Fatal(err)
			}
		})
	}
}

func productionIntegrityFixture(test *testing.T) Record {
	test.Helper()
	plan := helperPlan(test, "small", "pass", "integrity fixture")
	plan.CoordinatorID = "production-test-author"
	record, _ := fixtureTask(test, plan)
	if _, err := RunCheck(test.Context(), record.Directory, "focused"); err != nil {
		test.Fatal(err)
	}
	record, err := Load(record.Directory)
	if err != nil {
		test.Fatal(err)
	}
	return record
}

func TestProductionLoadRetainsStrictHistoryIntegrity(test *testing.T) {
	test.Parallel()
	// immutable-advisers: D5 binds advisers once at start, immutable for the
	// task's lifetime, like the plan and baseline.
	for _, mutation := range []string{"unknown-json", "duplicate-json", "trailing-json", "chain", "immutable-plan", "immutable-baseline", "immutable-advisers", "terminal-argv", "stored-timeout-zero", "missing-coordinator"} {
		test.Run(mutation, func(test *testing.T) {
			record := productionIntegrityFixture(test)
			tail := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", record.Revision))
			switch mutation {
			case "unknown-json":
				var fields map[string]jsontext.Value
				if err := json.Unmarshal(productionTaskRead(test, tail), &fields); err != nil {
					test.Fatal(err)
				}
				fields["production_unknown"] = jsontext.Value(`true`)
				data, err := json.Marshal(fields)
				if err != nil {
					test.Fatal(err)
				}
				productionTaskWrite(test, tail, data)
			case "duplicate-json":
				productionTaskWrite(test, tail, bytes.Replace(productionTaskRead(test, tail), []byte("{"), []byte(fmt.Sprintf(`{"version":%d,`, Version)), 1))
			case "trailing-json":
				productionTaskWrite(test, tail, append(productionTaskRead(test, tail), []byte("\n{}\n")...))
			case "chain":
				record.PreviousSHA256 = strings.Repeat("0", 64)
				data, err := encode(record)
				if err != nil {
					test.Fatal(err)
				}
				productionTaskWrite(test, tail, data)
			case "immutable-plan":
				productionRewriteHistory(test, record, func(revision int, historical *Record) {
					if revision == record.Revision {
						historical.Plan.Title += " changed only at tail"
					}
				})
			case "immutable-baseline":
				plan := helperPlan(test, "small", "pass")
				plan.CoordinatorID = "production-test-author"
				other, _ := fixtureTask(test, plan)
				productionRewriteHistory(test, record, func(revision int, historical *Record) {
					if revision == record.Revision {
						historical.Baseline = other.Baseline
					}
				})
			case "immutable-advisers":
				productionRewriteHistory(test, record, func(revision int, historical *Record) {
					if revision == record.Revision {
						historical.Advisers = map[string]string{"jev": "jev-1.14.0"}
					}
				})
			case "terminal-argv":
				productionRewriteHistory(test, record, func(revision int, historical *Record) {
					if revision == record.Revision {
						if len(historical.Checks) == 0 || historical.Checks[0].FinishedAt == nil {
							test.Fatal("missing terminal run in synthetic history")
						}
						historical.Checks[0].Argv = append(slices.Clone(historical.Checks[0].Argv), "not-in-plan")
					}
				})
			case "stored-timeout-zero":
				productionRewriteHistory(test, record, func(revision int, historical *Record) {
					historical.Plan.Checks[0].TimeoutSeconds = 0
				})
			case "missing-coordinator":
				productionRewriteHistory(test, record, func(revision int, historical *Record) {
					historical.Plan.CoordinatorID = ""
				})
			}
			before := productionTaskBytes(test, record.Directory)
			if _, err := Load(record.Directory); err == nil {
				test.Fatalf("Load accepted malformed %s fixture", mutation)
			}
			if !reflect.DeepEqual(productionTaskBytes(test, record.Directory), before) {
				test.Fatal("failed Load rewrote, defaulted, or migrated persisted bytes")
			}
		})
	}
}
