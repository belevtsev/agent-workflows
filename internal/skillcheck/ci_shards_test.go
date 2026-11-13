package skillcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// CI tests the Go packages in shard jobs (.github/workflows/ci.yml) whose
// steps and package lists are in .github/actions/go-test/action.yml. The
// required checks are result jobs that read the shard jobs' result, and a
// result covers only the matrix legs that exist: a shard missing from a
// matrix leaves its packages untested while the required check stays green.
const (
	ciWorkflowPath = ".github/workflows/ci.yml"
	goTestAction   = "./.github/actions/go-test"
	matrixShard    = "${{ matrix.shard }}"
	requireSuccess = `test "$RESULT" = success`
)

var (
	// shardCase matches the action's case statement over the shards, and
	// shardArm and defaultArm the two forms an arm of it may take.
	shardCase  = regexp.MustCompile(`(?s)case "\$SHARD" in\n(.*?)\n\s*esac`)
	shardArm   = regexp.MustCompile(`^(\d+)\) packages=.*;;$`)
	defaultArm = regexp.MustCompile(`^\*\) .*;;$`)
)

// stringList is a YAML value that is one string or a list of strings, as
// needs and runs-on are. Any other form, such as a runner group, is empty.
type stringList []string

func (l *stringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*l = stringList{node.Value}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			*l = append(*l, item.Value)
		}
	}
	return nil
}

type ciStep struct {
	Uses            string            `yaml:"uses"`
	If              string            `yaml:"if"`
	Run             string            `yaml:"run"`
	With            map[string]string `yaml:"with"`
	Env             map[string]string `yaml:"env"`
	ContinueOnError yaml.Node         `yaml:"continue-on-error"`
}

type ciJob struct {
	Name            string     `yaml:"name"`
	RunsOn          stringList `yaml:"runs-on"`
	Needs           stringList `yaml:"needs"`
	If              string     `yaml:"if"`
	ContinueOnError yaml.Node  `yaml:"continue-on-error"`
	Strategy        struct {
		Matrix yaml.Node `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []ciStep `yaml:"steps"`
}

// isSet reports whether a continue-on-error key is present and not false.
func isSet(node yaml.Node) bool { return node.Kind != 0 && node.Value != "false" }

// actionShards returns, sorted, the shards the action's case statement
// selects, or why the test cannot read them.
func actionShards(action []byte) ([]int, string) {
	blocks := shardCase.FindAllSubmatch(action, -1)
	if len(blocks) != 1 {
		return nil, fmt.Sprintf("found %d case statements over $SHARD, want 1", len(blocks))
	}
	var shards []int
	for _, line := range strings.Split(string(blocks[0][1]), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || defaultArm.MatchString(line) {
			continue
		}
		arm := shardArm.FindStringSubmatch(line)
		if arm == nil {
			return nil, fmt.Sprintf("cannot read the case arm %q: write each shard as `N) packages=... ;;` on one line", line)
		}
		n, err := strconv.Atoi(arm[1])
		if err != nil {
			return nil, err.Error()
		}
		shards = append(shards, n)
	}
	if len(shards) == 0 {
		return nil, "its case statement selects no shard"
	}
	sort.Ints(shards)
	return shards, ""
}

// matrixShards returns, sorted, the shards a job's matrix runs, or why the
// test cannot be sure of them.
func matrixShards(matrix yaml.Node) ([]int, string) {
	if matrix.Kind != yaml.MappingNode {
		return nil, "its matrix is not written out as a mapping"
	}
	var shards []int
	for i := 0; i+1 < len(matrix.Content); i += 2 {
		switch matrix.Content[i].Value {
		case "exclude":
			return nil, "its matrix has an exclude list, which can drop a shard"
		case "shard":
			if err := matrix.Content[i+1].Decode(&shards); err != nil {
				return nil, "its matrix shard is not a list of numbers"
			}
		}
	}
	sort.Ints(shards)
	return shards, ""
}

// shardJobProblems returns what stops job id, which uses the go-test action
// in step, from testing every shard in want.
func shardJobProblems(id string, job ciJob, step ciStep, want []int) []string {
	var problems []string
	run, unreadable := matrixShards(job.Strategy.Matrix)
	switch {
	case unreadable != "":
		problems = append(problems, fmt.Sprintf("job %s: %s", id, unreadable))
	case !slices.Equal(run, want):
		problems = append(problems, fmt.Sprintf("job %s runs shards %v, but the go-test action selects %v: "+
			"the packages of a shard no job runs are not tested", id, run, want))
	}
	if step.With["shard"] != matrixShard {
		problems = append(problems, fmt.Sprintf("job %s passes shard %q to the go-test action, want %s", id, step.With["shard"], matrixShard))
	}
	if step.If != "" {
		problems = append(problems, fmt.Sprintf("job %s runs the go-test action under if: %q: a skipped step passes without testing", id, step.If))
	}
	if isSet(job.ContinueOnError) || isSet(step.ContinueOnError) {
		problems = append(problems, fmt.Sprintf("job %s sets continue-on-error: a failed shard would count as passed", id))
	}
	return problems
}

// requiresSuccess reports whether a step of job fails unless the result of
// the job named needed is success.
func requiresSuccess(job ciJob, needed string) bool {
	want := "${{ needs." + needed + ".result }}"
	for _, step := range job.Steps {
		if step.Env["RESULT"] != want || step.If != "" || isSet(step.ContinueOnError) {
			continue
		}
		for _, line := range strings.Split(step.Run, "\n") {
			if strings.TrimSpace(line) == requireSuccess {
				return true
			}
		}
	}
	return false
}

// resultJobProblems returns what is wrong with the job named name, the
// required check of system: it must need one shard job, of that system, run
// with always() and fail unless that job's result is success.
func resultJobProblems(jobs, shardJobs map[string]ciJob, system string) []string {
	name := "Native Go / " + system
	var problems []string
	found := false
	for id, job := range jobs {
		if job.Name != name {
			continue
		}
		found = true
		who := fmt.Sprintf("job %s (%s)", id, name)
		var needed []string
		for _, need := range job.Needs {
			if _, ok := shardJobs[need]; ok {
				needed = append(needed, need)
			}
		}
		if len(needed) != 1 {
			problems = append(problems, fmt.Sprintf("%s needs %v, of which %d run shards, want 1", who, []string(job.Needs), len(needed)))
			continue
		}
		if runsOn := shardJobs[needed[0]].RunsOn; !slices.Equal(runsOn, stringList{system}) {
			problems = append(problems, fmt.Sprintf("%s needs %s, which runs on %v", who, needed[0], []string(runsOn)))
		}
		if job.If != "always()" {
			problems = append(problems, fmt.Sprintf("%s has if: %q, want always(): a skipped required check counts as passed", who, job.If))
		}
		if isSet(job.ContinueOnError) || !requiresSuccess(job, needed[0]) {
			problems = append(problems, fmt.Sprintf("%s has no step that fails unless needs.%s.result is success", who, needed[0]))
		}
	}
	if !found {
		problems = append(problems, "no job is named "+name+", a required check")
	}
	return problems
}

// ciShardProblems returns one line per broken rule, sorted: a job that uses
// the go-test action without running exactly the shards the action selects
// (shardJobProblems), and a required check that is not a sound result job
// (resultJobProblems).
func ciShardProblems(workflow, action []byte) []string {
	shards, unreadable := actionShards(action)
	if unreadable != "" {
		return []string{"go-test action: " + unreadable}
	}
	var file struct {
		Jobs map[string]ciJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflow, &file); err != nil {
		return []string{err.Error()}
	}
	var problems []string
	shardJobs := map[string]ciJob{}
	for id, job := range file.Jobs {
		for _, step := range job.Steps {
			if step.Uses == goTestAction {
				shardJobs[id] = job
				problems = append(problems, shardJobProblems(id, job, step, shards)...)
			}
		}
	}
	for _, system := range []string{"ubuntu-latest", "macos-latest"} {
		problems = append(problems, resultJobProblems(file.Jobs, shardJobs, system)...)
	}
	sort.Strings(problems)
	return problems
}

// oracle: in ci.yml every job that uses the go-test action runs, without an
// exclude list, a step condition or continue-on-error, exactly the shards
// the action's case statement selects, and passes matrix.shard to it; and
// each required check ("Native Go / ubuntu-latest", "Native Go /
// macos-latest") is a job that needs the shard job of its system, runs with
// always() and fails unless that job's result is success.
func TestCIShardJobsRunEveryShard(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(ciWorkflowPath)))
	if err != nil {
		t.Fatal(err)
	}
	action, err := os.ReadFile(filepath.Join(repoRoot, ".github", "actions", "go-test", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range ciShardProblems(workflow, action) {
		t.Errorf("%s: %s", ciWorkflowPath, problem)
	}
}

const (
	shardTestAction = `run: |
  case "$SHARD" in
    1) packages="$(grep -E "$first" <<<"$all")" ;;
    2) packages="$(grep -Ev "$first" <<<"$all")" ;;
    *) echo "unknown shard: $SHARD"; exit 1 ;;
  esac
`
	shardTestWorkflow = `jobs:
  go-linux:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        shard: [1, 2]
    steps:
      - uses: ./.github/actions/go-test
        with:
          shard: ${{ matrix.shard }}
  go-macos:
    runs-on: macos-latest
    strategy:
      fail-fast: false
      matrix:
        shard: [2, 1]
    steps:
      - uses: actions/checkout@v7
      - uses: ./.github/actions/go-test
        with:
          shard: ${{ matrix.shard }}
  linux-result:
    name: Native Go / ubuntu-latest
    needs: go-linux
    if: always()
    steps:
      - env:
          RESULT: ${{ needs.go-linux.result }}
        run: test "$RESULT" = success
  macos-result:
    name: Native Go / macos-latest
    needs: [go-macos]
    if: always()
    steps:
      - env:
          RESULT: ${{ needs.go-macos.result }}
        run: |
          echo "shards: $RESULT"
          test "$RESULT" = success
  lint:
    runs-on: [self-hosted, linux]
    needs: [go-linux, go-macos]
    strategy:
      matrix: ${{ fromJSON(needs.go-linux.outputs.matrix) }}
    steps:
      - run: golangci-lint run ./...
`
)

// oracle: a sound workflow has no problem, whatever valid form its other
// jobs take; each broken one is reported by the job and the rule it breaks.
func TestCIShardProblemsAreReported(t *testing.T) {
	if got := ciShardProblems([]byte(shardTestWorkflow), []byte(shardTestAction)); len(got) != 0 {
		t.Errorf("a sound workflow: got %q, want no problem", got)
	}
	const macosResult = "job macos-result (Native Go / macos-latest)"
	cases := []struct {
		name, old, new, want string
	}{
		{"shard missing from a matrix", "shard: [2, 1]", "shard: [1]",
			"job go-macos runs shards [1], but the go-test action selects [1 2]: the packages of a shard no job runs are not tested"},
		{"shard excluded", "shard: [2, 1]", "shard: [2, 1]\n        exclude: [{shard: 2}]",
			"job go-macos: its matrix has an exclude list, which can drop a shard"},
		{"computed matrix", "matrix:\n        shard: [2, 1]", "matrix: ${{ fromJSON(needs.plan.outputs.matrix) }}",
			"job go-macos: its matrix is not written out as a mapping"},
		{"shards as strings", "shard: [2, 1]", "shard: [two, one]",
			"job go-macos: its matrix shard is not a list of numbers"},
		{"constant shard", "- uses: actions/checkout@v7\n      - uses: ./.github/actions/go-test\n        with:\n          shard: ${{ matrix.shard }}",
			"- uses: ./.github/actions/go-test\n        with:\n          shard: 1",
			`job go-macos passes shard "1" to the go-test action, want ${{ matrix.shard }}`},
		{"conditional action step", "- uses: actions/checkout@v7\n      - uses: ./.github/actions/go-test",
			"- uses: ./.github/actions/go-test\n        if: matrix.shard != 2",
			`job go-macos runs the go-test action under if: "matrix.shard != 2": a skipped step passes without testing`},
		{"shard job continues on error", "runs-on: macos-latest", "runs-on: macos-latest\n    continue-on-error: true",
			"job go-macos sets continue-on-error: a failed shard would count as passed"},
		{"result job skipped after a failure", "needs: [go-macos]\n    if: always()", "needs: [go-macos]\n    if: ${{ !cancelled() }}",
			macosResult + ` has if: "${{ !cancelled() }}", want always(): a skipped required check counts as passed`},
		{"result job reads another result", "RESULT: ${{ needs.go-macos.result }}", "RESULT: ${{ needs.go-linux.result }}",
			macosResult + " has no step that fails unless needs.go-macos.result is success"},
		{"result step passes on a cancelled shard", "          test \"$RESULT\" = success\n", "          test \"$RESULT\" != failure\n",
			macosResult + " has no step that fails unless needs.go-macos.result is success"},
		{"result step never fails", "          test \"$RESULT\" = success\n", "          test \"$RESULT\" = success || true\n",
			macosResult + " has no step that fails unless needs.go-macos.result is success"},
		{"result job needs no shard job", "needs: [go-macos]\n    if: always()", "needs: [lint]\n    if: always()",
			macosResult + " needs [lint], of which 0 run shards, want 1"},
		{"result job of the other system", "needs: [go-macos]\n    if: always()", "needs: [go-linux]\n    if: always()",
			macosResult + " has no step that fails unless needs.go-linux.result is success"},
		{"required name gone", "name: Native Go / macos-latest", "name: Go / macos-latest",
			"no job is named Native Go / macos-latest, a required check"},
	}
	for _, tc := range cases {
		if !strings.Contains(shardTestWorkflow, tc.old) {
			t.Fatalf("%s: the workflow does not hold %q", tc.name, tc.old)
		}
		got := ciShardProblems([]byte(strings.Replace(shardTestWorkflow, tc.old, tc.new, 1)), []byte(shardTestAction))
		if !slices.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want it to hold %q", tc.name, got, tc.want)
		}
	}
	wrongSystem := strings.Replace(shardTestWorkflow, "needs: [go-macos]\n    if: always()", "needs: [go-linux]\n    if: always()", 1)
	wrongSystem = strings.Replace(wrongSystem, "RESULT: ${{ needs.go-macos.result }}", "RESULT: ${{ needs.go-linux.result }}", 1)
	want := []string{macosResult + " needs go-linux, which runs on [ubuntu-latest]"}
	if got := ciShardProblems([]byte(wrongSystem), []byte(shardTestAction)); !slices.Equal(got, want) {
		t.Errorf("result job that reads the other system's shards: got %q, want %q", got, want)
	}
}

// oracle: the shards are read only from a case statement over $SHARD whose
// arms are `N) packages=... ;;` lines and a default; any other arm, which
// could select a shard the test does not see, is reported with its text.
func TestUnreadableShardArmsAreReported(t *testing.T) {
	const rewrite = ": write each shard as `N) packages=... ;;` on one line"
	cases := []struct {
		name, old, new, want string
	}{
		{"two shards in one arm", "    2) packages=", "    2|3) packages=",
			`go-test action: cannot read the case arm "2|3) packages=\"$(grep -Ev \"$first\" <<<\"$all\")\" ;;"` + rewrite},
		{"arm on its own line", "    2) packages=", "    2)\n      packages=",
			`go-test action: cannot read the case arm "2)"` + rewrite},
		{"quoted arm", "    2) packages=", "    \"2\") packages=",
			`go-test action: cannot read the case arm "\"2\") packages=\"$(grep -Ev \"$first\" <<<\"$all\")\" ;;"` + rewrite},
		{"no case statement", `case "$SHARD" in`, `case "$OTHER" in`,
			"go-test action: found 0 case statements over $SHARD, want 1"},
		{"only a default arm", "    1) packages=\"$(grep -E \"$first\" <<<\"$all\")\" ;;\n    2) packages=\"$(grep -Ev \"$first\" <<<\"$all\")\" ;;\n", "",
			"go-test action: its case statement selects no shard"},
	}
	for _, tc := range cases {
		if !strings.Contains(shardTestAction, tc.old) {
			t.Fatalf("%s: the action does not hold %q", tc.name, tc.old)
		}
		got := ciShardProblems([]byte(shardTestWorkflow), []byte(strings.Replace(shardTestAction, tc.old, tc.new, 1)))
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: got %q, want [%q]", tc.name, got, tc.want)
		}
	}
	third := strings.Replace(shardTestAction, "    *) echo", "    3) packages=\"$all\" ;;\n    *) echo", 1)
	got := ciShardProblems([]byte(shardTestWorkflow), []byte(third))
	if len(got) != 2 || !strings.HasPrefix(got[0], "job go-linux runs shards [1 2], but the go-test action selects [1 2 3]") {
		t.Errorf("an arm no matrix runs: got %q, want one problem for each shard job", got)
	}
}
