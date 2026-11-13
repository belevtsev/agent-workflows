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
)

// shardArm matches one arm of the action's case statement over the shards.
var shardArm = regexp.MustCompile(`(?m)^\s*(\d+)\) packages=`)

type ciJob struct {
	Name     string `yaml:"name"`
	RunsOn   string `yaml:"runs-on"`
	Needs    string `yaml:"needs"`
	If       string `yaml:"if"`
	Strategy struct {
		Matrix struct {
			Shard []int `yaml:"shard"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []struct {
		Uses string            `yaml:"uses"`
		Run  string            `yaml:"run"`
		Env  map[string]string `yaml:"env"`
	} `yaml:"steps"`
}

// requiresSuccess reports whether a step of job fails unless the result of
// the job it needs is success.
func requiresSuccess(job ciJob) bool {
	want := "${{ needs." + job.Needs + ".result }}"
	for _, step := range job.Steps {
		if step.Env["RESULT"] == want && strings.Contains(step.Run, `test "$RESULT" = success`) {
			return true
		}
	}
	return false
}

// resultJobProblems returns what is wrong with the job named name, the
// required check of system: it must need a shard job of that system, run
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
		needed, ok := shardJobs[job.Needs]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("job %s (%s) needs %q, which runs no shard", id, name, job.Needs))
		case needed.RunsOn != system:
			problems = append(problems, fmt.Sprintf("job %s (%s) needs %s, which runs on %s", id, name, job.Needs, needed.RunsOn))
		}
		if job.If != "always()" {
			problems = append(problems, fmt.Sprintf("job %s (%s) has if: %q, want always(): a skipped required check counts as passed", id, name, job.If))
		}
		if !requiresSuccess(job) {
			problems = append(problems, fmt.Sprintf("job %s (%s) has no step that fails unless needs.%s.result is success", id, name, job.Needs))
		}
	}
	if !found {
		problems = append(problems, "no job is named "+name+", a required check")
	}
	return problems
}

// ciShardProblems returns one line per broken rule, sorted: a job that uses
// the go-test action without running exactly the shards the action selects,
// and a required check that is not a sound result job (resultJobProblems).
func ciShardProblems(workflow, action []byte) []string {
	var shards []int
	for _, arm := range shardArm.FindAllSubmatch(action, -1) {
		n, err := strconv.Atoi(string(arm[1]))
		if err != nil {
			return []string{"go-test action: " + err.Error()}
		}
		shards = append(shards, n)
	}
	if len(shards) == 0 {
		return []string{"go-test action: found no `N) packages=` arm, so it selects no shard"}
	}
	sort.Ints(shards)
	var file struct {
		Jobs map[string]ciJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflow, &file); err != nil {
		return []string{ciWorkflowPath + ": " + err.Error()}
	}
	var problems []string
	shardJobs := map[string]ciJob{}
	for id, job := range file.Jobs {
		for _, step := range job.Steps {
			if step.Uses != goTestAction {
				continue
			}
			shardJobs[id] = job
			run := slices.Clone(job.Strategy.Matrix.Shard)
			sort.Ints(run)
			if !slices.Equal(run, shards) {
				problems = append(problems, fmt.Sprintf("job %s runs shards %v, but the go-test action selects %v: "+
					"the packages of a shard no job runs are not tested", id, run, shards))
			}
		}
	}
	for _, system := range []string{"ubuntu-latest", "macos-latest"} {
		problems = append(problems, resultJobProblems(file.Jobs, shardJobs, system)...)
	}
	sort.Strings(problems)
	return problems
}

// oracle: in ci.yml every job that uses the go-test action runs exactly the
// shards the action's case statement selects, and each required check
// ("Native Go / ubuntu-latest", "Native Go / macos-latest") is a job that
// needs the shard job of its system, runs with always() and fails unless that
// job's result is success.
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

// oracle: a sound workflow has no problem; each broken one is reported by the
// job and the rule it breaks.
func TestCIShardProblemsAreReported(t *testing.T) {
	const action = "case \"$SHARD\" in\n  1) packages=a ;;\n  2) packages=b ;;\n  *) exit 1 ;;\nesac\n"
	const workflow = `jobs:
  go-linux:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        shard: [1, 2]
    steps:
      - uses: ./.github/actions/go-test
  go-macos:
    runs-on: macos-latest
    strategy:
      matrix:
        shard: [2, 1]
    steps:
      - uses: ./.github/actions/go-test
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
    needs: go-macos
    if: always()
    steps:
      - env:
          RESULT: ${{ needs.go-macos.result }}
        run: test "$RESULT" = success
`
	if got := ciShardProblems([]byte(workflow), []byte(action)); len(got) != 0 {
		t.Errorf("a sound workflow: got %q, want no problem", got)
	}
	cases := []struct {
		name, old, new, want string
	}{
		{"shard missing from a matrix", "shard: [2, 1]", "shard: [1]",
			"job go-macos runs shards [1], but the go-test action selects [1 2]: the packages of a shard no job runs are not tested"},
		{"result job skipped after a failure", "needs: go-macos\n    if: always()", "needs: go-macos\n    if: ${{ !cancelled() }}",
			`job macos-result (Native Go / macos-latest) has if: "${{ !cancelled() }}", want always(): a skipped required check counts as passed`},
		{"result job reads another result", "RESULT: ${{ needs.go-macos.result }}", "RESULT: ${{ needs.go-linux.result }}",
			"job macos-result (Native Go / macos-latest) has no step that fails unless needs.go-macos.result is success"},
		{"result step passes on a cancelled shard", "RESULT: ${{ needs.go-macos.result }}\n        run: test \"$RESULT\" = success",
			"RESULT: ${{ needs.go-macos.result }}\n        run: test \"$RESULT\" != failure",
			"job macos-result (Native Go / macos-latest) has no step that fails unless needs.go-macos.result is success"},
		{"required name gone", "name: Native Go / macos-latest", "name: Go / macos-latest",
			"no job is named Native Go / macos-latest, a required check"},
	}
	for _, tc := range cases {
		if !strings.Contains(workflow, tc.old) {
			t.Fatalf("%s: the workflow does not hold %q", tc.name, tc.old)
		}
		got := ciShardProblems([]byte(strings.Replace(workflow, tc.old, tc.new, 1)), []byte(action))
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: got %q, want [%q]", tc.name, got, tc.want)
		}
	}
	wrongSystem := strings.Replace(workflow, "needs: go-macos\n    if: always()", "needs: go-linux\n    if: always()", 1)
	wrongSystem = strings.Replace(wrongSystem, "RESULT: ${{ needs.go-macos.result }}", "RESULT: ${{ needs.go-linux.result }}", 1)
	want := "job macos-result (Native Go / macos-latest) needs go-linux, which runs on ubuntu-latest"
	if got := ciShardProblems([]byte(wrongSystem), []byte(action)); len(got) != 1 || got[0] != want {
		t.Errorf("result job of the other system: got %q, want [%q]", got, want)
	}
	if got := ciShardProblems([]byte(workflow), []byte("run: go test ./...\n")); len(got) != 1 ||
		got[0] != "go-test action: found no `N) packages=` arm, so it selects no shard" {
		t.Errorf("an action without shards: got %q", got)
	}
}
