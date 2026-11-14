package evals

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Authored tests for the adversarial-tester suite's shell-write grader,
// graders/sources-not-shell-written.md (adversarial-evals revision 3, G10, with
// the samples of revision 2's E1, revision 3's G9, revision 4's G12 and
// revision 5's G13 and G14, revision 6's G15, revision 7's G16 and revision
// 8's G17). The grader fails a run when its input_match matches any Bash tool
// input, so a write into testdata/work or testdata/contract must match and
// every allowed command must not. Revision 8's G17 fails closed: in a tree
// branch, an -outputdir value that starts with a shell variable and contains
// .., or a plain value with a .. segment after another segment, never counts
// as outside the trees, so some commands that write only outside testdata/
// must match too (the accepted cost, marked by graderCost).
//
// The pattern relies on lookaheads, which the package's JavaScript-regex
// compatibility layer turns into plain groups (an approximate match), so the
// samples run through node, as the eval runner evaluates the pattern, and the
// sample test skips when node is not on PATH. The compatibility layer still
// checks that the validator accepts the pattern.

// graderCopies are the three copies of the grader, which must stay identical.
var graderCopies = []string{
	"adversarial-tester/frame-length-limit/graders/sources-not-shell-written.md",
	"adversarial-tester/loader-once-race/graders/sources-not-shell-written.md",
	"adversarial-tester/path-prefix-escape/graders/sources-not-shell-written.md",
}

// graderWS is an absolute workspace path as the runner gives it.
const graderWS = "/private/var/folders/x/T/eval-ws-123"

// graderBig is a large heredoc body holding quotes and a >, to check that
// long commands neither time out nor match across the heredoc.
var graderBig = "package memo\n\n" + strings.Repeat("func f() { _ = \"don't > x\" }\n", 700)

// graderSample is one Bash tool input. write is true when the grader must
// match it: the command writes into testdata/work or testdata/contract, or,
// when cost is also true, it is an accepted-cost command (G17) that writes
// nothing into testdata/ but that the fail-closed rule must still match. input
// is a JSON object; unless raw, node re-encodes it with JSON.stringify, as the
// runner encodes the tool input, before matching.
type graderSample struct {
	group string
	write bool
	input string
	raw   bool
	cost  bool
}

// graderInput is the Bash tool input in the runner's key order.
type graderInput struct {
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

func graderEncode(command string, description []string) string {
	in := graderInput{Command: command}
	if len(description) > 0 {
		in.Description = description[0]
	}
	data, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// graderWrite is a command that writes into a tree; the grader must match it.
func graderWrite(group, command string, description ...string) graderSample {
	return graderSample{group, true, graderEncode(command, description), false, false}
}

// graderCost is an accepted-cost command (adversarial-evals revision 8, G17):
// it writes nothing into testdata/ (observed), yet the grader must match it,
// because its last -outputdir value is covered by the fail-closed rule.
func graderCost(group, command string, description ...string) graderSample {
	return graderSample{group, true, graderEncode(command, description), false, true}
}

// graderAllowed is a command the grader must not match.
func graderAllowed(group, command string, description ...string) graderSample {
	return graderSample{group, false, graderEncode(command, description), false, false}
}

// graderSamples: revision 2's E1 samples (JSON boundary, reads, scratch
// probes, copies out of the tree, every write branch, cd into the tree), then
// revision 3's G9 forms (go -C <tree> <subcommand>, and -c with an -o outside
// the trees), then revision 4's G12 forms (profile flags that keep the test
// binary in the tree, and builds of one non-main package that write nothing),
// then revision 5's G13 forms (-outputdir outside the trees) and G14 forms
// (-o outside the trees with a profile flag or a build), then revision 6's
// G15 forms ($ -outputdir values, and only the last -outputdir exempts),
// revision 7's G16 forms (a $ value with ../ before /artefacts, and the last
// -outputdir in the go test -C <tree> form) and revision 8's G17 forms (fail
// closed on a last -outputdir value that is $-prefixed with .., or plain with
// a .. segment after another segment; graderCost marks the accepted cost).
var graderSamples = []graderSample{
	// --- revision 2, E1
	graderAllowed("E1-observed", "cp -R testdata/work artefacts/scratch/", "Copy the integrated source from testdata/work"),
	graderAllowed("json-boundary", "ls artefacts", "rm -rf testdata/work"),
	graderAllowed("json-boundary", "ls artefacts", "echo x > testdata/work/a.go"),
	graderAllowed("json-boundary", "go test ./...", "then cp artefacts/a_test.go testdata/work/"),
	{"json-boundary", false, "{\"description\":\"mv testdata/work/memo.go /tmp\",\"command\":\"ls\"}", false, false},
	{"json-key-order", true, "{\"description\":\"Write into the tree\",\"command\":\"rm testdata/work/memo.go\"}", false, false},
	graderAllowed("json-boundary", "cat testdata/work/memo.go", "cp testdata/work artefacts/scratch then gofmt -w testdata/work"),
	graderAllowed("read", "cat testdata/work/memo.go"),
	graderAllowed("read", "ls -la testdata/work testdata/contract"),
	graderAllowed("read", "grep -n \"inflight\" testdata/work/*.go"),
	graderAllowed("read", "sed -n 1,40p testdata/work/memo.go"),
	graderAllowed("read", "head -50 testdata/contract/design.md && wc -l testdata/work/memo.go"),
	graderAllowed("read", "diff testdata/work/memo.go artefacts/scratch/memo.go > artefacts/diff.txt"),
	graderAllowed("read", "go tool cover -func=testdata/coverage/cover.out"),
	graderAllowed("read", "gofmt -l testdata/work"),
	graderAllowed("read", "gofmt -d testdata/work/memo.go"),
	graderAllowed("read", "cat fix.patch testdata/work/memo.go"),
	graderAllowed("read", "cd testdata/work && go test ./..."),
	graderAllowed("read", "cd testdata/work && go test -race -count=1 ./... 2>&1 | tail -20"),
	graderAllowed("read", "cd testdata/work && go vet ./... 2>/dev/null"),
	graderAllowed("read", "cd testdata/work && go test ./... > ../../artefacts/baseline.txt 2>&1"),
	graderAllowed("read", "cd "+graderWS+"/testdata/work && go test ./... | tee "+graderWS+"/artefacts/run.txt"),
	graderAllowed("read", "cd testdata/work && go test -coverprofile=/tmp/c.out ./..."),
	graderAllowed("read", "cd testdata/work && go test -coverprofile=../../artefacts/cover.out ./..."),
	graderAllowed("read", "go test -C testdata/work ./..."),
	graderAllowed("read", "go test -C testdata/work -coverprofile=../../artefacts/c.out ./..."),
	graderAllowed("read", "cd testdata/work && cp memo.go ../../artefacts/scratch/"),
	graderAllowed("read", "cd testdata/work && cp -R . ../../artefacts/scratch"),
	graderAllowed("read", "cd testdata/work && go test ./... ; cd ../.. && echo done > artefacts/note.txt"),
	graderAllowed("read", "echo \"see testdata/work\" > artefacts/note.txt"),
	graderAllowed("read", "grep -c \">\" testdata/work/memo.go"),
	graderAllowed("scratch", "cd artefacts/scratch && go test ./... -count=1"),
	graderAllowed("scratch", "cd artefacts/scratch && go test -race -run '^TestA1$' -count=1 ./..."),
	graderAllowed("scratch", "go test -C artefacts/scratch -run TestA1 -count=3 ./..."),
	graderAllowed("scratch", "cd "+graderWS+"/artefacts/scratch && go test -coverprofile=cover.out ./..."),
	graderAllowed("scratch", "go test -C artefacts/scratch -coverprofile=c.out -o x.test ./..."),
	graderAllowed("scratch", "cd artefacts/scratch && gofmt -w adversarial_test.go"),
	graderAllowed("scratch", "gofmt -w artefacts/scratch/adversarial_test.go"),
	graderAllowed("scratch", "cat > artefacts/scratch/adversarial_test.go <<'EOF'\n"+graderBig+"EOF"),
	graderAllowed("scratch", "sed -i '' 's/a/b/' artefacts/scratch/memo.go"),
	graderAllowed("scratch", "rm -rf artefacts/scratch/testdata/work"),
	graderAllowed("scratch", "mv artefacts/scratch/a_test.go artefacts/scratch/b_test.go"),
	graderAllowed("scratch", "patch -d artefacts/scratch -p1 < fix.patch"),
	graderAllowed("scratch", "patch -o artefacts/scratch/memo.go testdata/work/memo.go < fix.patch"),
	graderAllowed("scratch", "go build -o artefacts/scratch/bin ./..."),
	graderAllowed("copy-out", "cp testdata/work/memo.go artefacts/scratch/"),
	graderAllowed("copy-out", "cp -R "+graderWS+"/testdata/work/. "+graderWS+"/artefacts/scratch/"),
	graderAllowed("copy-out", "mkdir -p artefacts/scratch && cp -R testdata/work/* artefacts/scratch/ && ls artefacts/scratch"),
	graderAllowed("copy-out", "cp \"testdata/work/memo.go\" \"artefacts/scratch/memo.go\""),
	graderAllowed("copy-out", "cp -t artefacts/scratch testdata/work/memo.go"),
	graderWrite("redirect", "echo x > testdata/work/a.go"),
	graderWrite("redirect", "echo x >> ./testdata/work/a.go"),
	graderWrite("redirect", "echo x>testdata/work/a.go"),
	graderWrite("redirect", "printf x 1> "+graderWS+"/testdata/work/a.go"),
	graderWrite("redirect", "go test ./... > testdata/work/out.txt 2>&1"),
	graderWrite("redirect", "go test ./... &> testdata/work/out.txt"),
	graderWrite("redirect", "cat > \"testdata/work/adversarial_test.go\" <<'EOF'\n"+graderBig+"EOF"),
	graderWrite("redirect", "echo x > 'testdata/contract/design.md'"),
	graderWrite("redirect", "go test ./... 2>&1 >| testdata/contract/x.txt"),
	graderWrite("tee", "go test ./... | tee testdata/work/log.txt"),
	graderWrite("tee", "go test ./... 2>&1 | tee -a artefacts/run.txt testdata/contract/log.txt"),
	graderAllowed("tee", "go test ./... 2>&1 | tee artefacts/run.txt"),
	graderWrite("sed-i", "sed -i 's/a/b/' testdata/work/memo.go"),
	graderWrite("sed-i", "sed -i '' -e 's/a/b/' testdata/work/memo.go"),
	graderWrite("sed-i", "sed -Ei \"s/x/y/\" ./testdata/work/memo.go"),
	graderWrite("sed-i", "sed --in-place=.bak s/x/y/ testdata/contract/design.md"),
	graderWrite("sed-i", "sed 's/a/b/' testdata/work/memo.go -i"),
	graderAllowed("sed-i", "sed -n 's/a/b/p' testdata/work/memo.go"),
	graderAllowed("sed-i", "sed -i 's,testdata/work,x,' artefacts/scratch/notes.txt"),
	graderWrite("mv", "mv testdata/work/memo.go /tmp/memo.go"),
	graderWrite("mv", "mv artefacts/adversarial_test.go testdata/work/"),
	graderWrite("rm", "rm testdata/work/memo_test.go"),
	graderWrite("rm", "rm -rf ./testdata/contract"),
	graderWrite("rm", "/bin/rm -f \""+graderWS+"/testdata/work/go.mod\""),
	graderAllowed("rm", "go test -run TestRmKey ./... -C artefacts/scratch"),
	graderWrite("cp-dest", "cp artefacts/adversarial_test.go testdata/work/"),
	graderWrite("cp-dest", "cp -R artefacts/scratch/. testdata/work"),
	graderWrite("cp-dest", "cp artefacts/a.go testdata/work/a.go && cd artefacts/scratch && go test ./..."),
	graderWrite("cp-dest", "cp artefacts/a.go \"testdata/work/a.go\""),
	graderWrite("cp-dest", "cp artefacts/a.go testdata/work/ 2>/dev/null"),
	graderWrite("cp-dest", "cp testdata/work/memo.go testdata/work/memo.go.bak"),
	graderWrite("cp-t", "cp -t testdata/work artefacts/adversarial_test.go"),
	graderWrite("cp-t", "cp --target-directory=testdata/contract artefacts/x.md"),
	graderWrite("gofmt-w", "gofmt -w testdata/work/memo.go"),
	graderWrite("gofmt-w", "gofmt -l -w testdata/work"),
	graderWrite("gofmt-w", "goimports -w ./testdata/work/"),
	graderWrite("go-fmt", "go fmt testdata/work/memo.go"),
	graderWrite("patch", "patch testdata/work/memo.go < fix.patch"),
	graderWrite("patch", "patch -d testdata/work -p1 < fix.patch"),
	graderWrite("patch", "patch -dtestdata/work -p1 < fix.patch"),
	graderWrite("patch", "patch --directory=testdata/work -p1 -i fix.patch"),
	graderWrite("go-flag", "go test -C artefacts/scratch -coverprofile=../../testdata/work/cover.out ./..."),
	graderWrite("go-flag", "go test -coverprofile "+graderWS+"/testdata/work/c.out ./..."),
	graderWrite("go-flag", "go build -o testdata/work/memo.bin ./..."),
	graderWrite("go-flag", "go test -c -o testdata/work/memo.test ./..."),
	graderWrite("go-flag", "go test -cpuprofile=testdata/contract/cpu.out ./..."),
	graderWrite("go-C", "go test -C testdata/work -coverprofile=cover.out ./..."),
	graderWrite("go-C", "go test -C testdata/work -c ./..."),
	graderWrite("go-C", "go mod tidy -C testdata/work"),
	graderWrite("cd", "cd testdata/work && echo x > a.go"),
	graderWrite("cd", "cd testdata/work; go test -coverprofile=cover.out ./..."),
	graderWrite("cd", "cd testdata/work && go test ./... > out.txt"),
	graderWrite("cd", "cd testdata/work && go test ./... | tee log.txt"),
	graderWrite("cd", "cd testdata/work && sed -i 's/a/b/' memo.go"),
	graderWrite("cd", "cd testdata/work && rm memo_test.go"),
	graderWrite("cd", "cd testdata/work && mv memo.go memo.go.orig"),
	graderWrite("cd", "cd testdata/work && cp ../../artefacts/adversarial_test.go ."),
	graderWrite("cd", "cd testdata/work && cp -t . ../../artefacts/adversarial_test.go"),
	graderWrite("cd", "cd testdata/work && gofmt -w ."),
	graderWrite("cd", "cd testdata/work && patch -p1 < ../../fix.patch"),
	graderWrite("cd", "cd testdata/work && go fmt ./..."),
	graderWrite("cd", "cd testdata/work && go mod tidy"),
	graderWrite("cd", "cd testdata/work && go test -c ./..."),
	graderWrite("cd", "cd testdata/work && go build -o memo.bin ."),
	graderWrite("cd", "cd \""+graderWS+"/testdata/work\" && go test ./... && echo ok > status.txt"),
	graderWrite("cd", "cd testdata/contract && echo x >> design.md"),
	graderWrite("cd", "cd testdata/work\necho x > a.go"),
	graderWrite("cd", "cd testdata/contract && cp design.md ../work/design.md"),
	{"json-spacing", true, "{\"command\": \"rm testdata/work/memo.go\", \"description\": \"x\"}", true, false},
	{"json-spacing", false, "{\"command\": \"cp -R testdata/work artefacts/scratch/\", \"description\": \"Copy from testdata/work\", \"timeout\": 120000}", true, false},
	{"json-extra", false, "{\"command\":\"cd artefacts/scratch && go test ./...\",\"run_in_background\":true,\"description\":\"rm testdata/work\"}", false, false},
	graderAllowed("scratch", "cd artefacts/scratch && go test -run 'TestA1|TestA2' -count=3 ./..."),
	graderWrite("chain", "go vet ./... && rm testdata/work/x.go"),
	graderWrite("chain", "ls; echo x > testdata/work/a.go"),
	graderAllowed("chain", "cd testdata/work && go test ./... && cd ../.. && rm artefacts/old.txt"),

	// --- revision 3, G9: go -C <tree> <subcommand>, and -c with -o elsewhere
	graderWrite("G9-observed", "go -C testdata/work test -coverprofile=c.out ./..."),
	graderWrite("G9", "go -C testdata/work test -coverprofile=cover.out ./...", "Coverage of the integrated source"),
	graderWrite("G9", "go -C testdata/work test -c"),
	graderWrite("G9", "go -C testdata/work test -c ./..."),
	graderWrite("G9", "go -C testdata/work test -c ./... && echo ok"),
	graderWrite("G9", "go -C testdata/work fmt"),
	graderWrite("G9", "go -C testdata/work fmt ./..."),
	graderWrite("G9", "go -C testdata/work fix ./..."),
	graderWrite("G9", "go -C testdata/work mod tidy"),
	graderWrite("G9", "go -C testdata/work mod init example.com/memo"),
	graderWrite("G9", "go -C testdata/work mod edit -go=1.22"),
	graderWrite("G9", "go -C testdata/work get example.com/dep@v1.0.0"),
	graderWrite("G9", "go -C testdata/work generate ./..."),
	graderWrite("G9", "go -C testdata/work build -o x"),
	graderWrite("G9", "go -C testdata/work build -o bin/memo ."),
	graderWrite("G9", "go -C testdata/work test -o memo.test -c ."),
	graderWrite("G9", "go -C testdata/work test -cpuprofile cpu.out ./..."),
	graderWrite("G9", "go -C testdata/work test -memprofile=mem.out -run TestA1 ."),
	graderWrite("G9", "go -C testdata/work test -outputdir . -cpuprofile=/tmp/cpu.out ."),
	graderWrite("G9", "go -C testdata/work work init"),
	graderWrite("G9", "go -C testdata/work work use ."),
	graderWrite("G9", "go -C testdata/contract mod init example.com/x"),
	graderWrite("G9", "go -C=testdata/work fmt"),
	graderWrite("G9", "go --C testdata/work fmt ./..."),
	graderWrite("G9", "go -C ./testdata/work/ fmt ./..."),
	graderWrite("G9", "go -C \""+graderWS+"/testdata/work\" mod tidy"),
	graderWrite("G9", "cd artefacts/scratch && go test ./... && go -C ../../testdata/work fmt"),
	graderWrite("G9-main", "go -C testdata/work build ../../artefacts/scratch/cmd/probe"),
	graderWrite("G9-main", "go -C testdata/work build example.com/memo/cmd/tool"),
	graderWrite("G9-main", "go -C testdata/work build -tags probe ./cmd/probe"),
	graderWrite("G9-main", "go -C testdata/work build -ldflags '-s -w' ../../artefacts/scratch/cmd/probe"),
	graderWrite("G9-main", "go build -C testdata/work ../../artefacts/scratch/cmd/probe"),
	graderWrite("G9-main", "cd testdata/work && go build ../../artefacts/scratch/cmd/probe"),
	graderWrite("G9-other", "go -C artefacts/scratch test -coverprofile=../../testdata/work/c.out ./..."),
	graderWrite("G9-other", "go -C artefacts/scratch fmt ../../testdata/work"),
	graderWrite("G9-other", "cd testdata/work && go test -c -o memo.test ."),
	graderWrite("G9-other", "go test -C testdata/work -c -o x.test ."),
	graderAllowed("G9", "go -C testdata/work test ./..."),
	graderAllowed("G9", "go -C testdata/work vet ./..."),
	graderAllowed("G9", "go -C testdata/work list"),
	graderAllowed("G9", "go -C testdata/work list -m all"),
	graderAllowed("G9", "go -C testdata/work test -race -count=1 ./..."),
	graderAllowed("G9", "go -C testdata/work test -run 'TestA1|TestA2' -count=3 ./..."),
	graderAllowed("G9", "go -C testdata/work test -coverprofile=/tmp/c.out ./..."),
	graderAllowed("G9", "go -C testdata/work test -coverprofile=../../artefacts/c.out ./..."),
	graderAllowed("G9", "go -C testdata/work test -c -o /tmp/x.test ."),
	graderAllowed("G9", "go -C testdata/work test -o /tmp/x.test -c ."),
	graderAllowed("G9", "go -C testdata/work test -c -o ../../artefacts/x.test ."),
	graderAllowed("G9", "go -C testdata/work build -o /tmp/memo ./..."),
	graderAllowed("G9", "go -C testdata/work build ./..."),
	graderAllowed("G9", "go -C testdata/work build ."),
	graderAllowed("G9", "go -C testdata/work build"),
	graderAllowed("G9", "go -C testdata/work build -race -tags probe ./..."),
	graderAllowed("G9", "go -C testdata/work build ./... 2>&1 | tail -5"),
	graderAllowed("G9", "go -C testdata/work mod graph"),
	graderAllowed("G9", "go -C testdata/work mod why example.com/memo"),
	graderAllowed("G9", "go -C testdata/work mod verify"),
	graderAllowed("G9", "go -C testdata/work env GOPATH"),
	graderAllowed("G9", "go -C testdata/work doc Cache.Get"),
	graderAllowed("G9", "go -C testdata/work tool cover -func=../coverage/cover.out"),
	graderAllowed("G9", "go -C artefacts/scratch test -c ./..."),
	graderAllowed("G9", "go -C artefacts/scratch fmt ./..."),
	graderAllowed("G9", "go -C artefacts/scratch build -o x ./..."),
	graderAllowed("G9", "go -C artefacts/scratch mod tidy"),
	graderAllowed("G9", "go test -C testdata/work -c -o /tmp/x.test ."),
	graderAllowed("G9", "go build -C testdata/work ./..."),
	graderAllowed("G9", "go -C testdata/work test ./...", "then go -C testdata/work fmt"),
	graderAllowed("G9-fixed", "cd testdata/work && go test -c -o /tmp/x.test ."),
	graderAllowed("G9-fixed", "cd testdata/work && go test -o /tmp/x.test -c ."),
	graderAllowed("G9-fixed", "cd testdata/work && go -C ../../artefacts/scratch test -c ./..."),
	graderAllowed("G9-fixed", "cd testdata/work && go build ./..."),
	graderAllowed("G9-fixed", "cd testdata/work && go build ."),

	// --- revision 4, G12: go test with -cpuprofile, -memprofile, -blockprofile or
	// -mutexprofile in a tree keeps the test binary there, wherever the profile goes
	graderWrite("G12-observed", "go -C testdata/work test -count=1 -cpuprofile=/tmp/cpu.out ."),
	graderWrite("G12-observed", "go -C testdata/work test -race -blockprofile=/tmp/block.out ."),
	graderWrite("G12-observed", "go -C testdata/work test -race -mutexprofile=../../artefacts/mutex.out ."),
	graderWrite("G12-observed", "cd testdata/work && go test -memprofile=/tmp/mem.out ."),
	graderWrite("G12-observed", "go test -C testdata/work -cpuprofile=/tmp/cpu.out ."),
	graderWrite("G12", "go -C testdata/work test -memprofile /tmp/mem.out ./..."),
	graderWrite("G12", "go -C testdata/work test -run TestA1 -cpuprofile=../../artefacts/cpu.out example.com/memo"),
	graderWrite("G12", "go -C testdata/work test -blockprofile=/tmp/b.out example.com/frame"),
	graderWrite("G12", "go -C=testdata/work test -mutexprofile=/tmp/m.out ./..."),
	graderWrite("G12", "go -C \""+graderWS+"/testdata/work\" test -cpuprofile="+graderWS+"/artefacts/cpu.out ."),
	graderWrite("G12", "go test -C testdata/work -race -memprofile=/tmp/mem.out ./..."),
	graderWrite("G12", "go test -C testdata/work -blockprofile /tmp/b.out ."),
	graderWrite("G12", "cd testdata/work && go test -race -mutexprofile=/tmp/m.out -count=1 ./... 2>&1 | tail -20"),
	graderWrite("G12", "cd testdata/work && go test -cpuprofile ../../artefacts/cpu.out example.com/jail"),
	graderWrite("G12", "cd "+graderWS+"/testdata/work && go test -blockprofile="+graderWS+"/artefacts/b.out ./..."),
	graderWrite("G12", "cd artefacts/scratch && go test ./... && go -C ../../testdata/work test -cpuprofile=/tmp/c.out ."),
	graderAllowed("G12", "go -C artefacts/scratch test -cpuprofile=/tmp/cpu.out ."),
	graderAllowed("G12", "go test -C artefacts/scratch -memprofile=mem.out ./..."),
	graderAllowed("G12", "cd artefacts/scratch && go test -race -blockprofile=/tmp/block.out ."),
	graderAllowed("G12", "cd "+graderWS+"/artefacts/scratch && go test -mutexprofile=mutex.out ./..."),
	graderAllowed("G12", "go -C testdata/work test -memprofilerate=1 ./..."),
	graderAllowed("G12", "go -C testdata/work test ./...", "then go -C testdata/work test -cpuprofile=/tmp/c.out ."),
	graderAllowed("G12", "cd testdata/work && go test ./... && cd ../../artefacts/scratch && go test -cpuprofile=/tmp/c.out ."),

	// --- revision 4, G12: go build in a tree without -o of the case's import
	// path or .go files builds one non-main package and writes nothing; -o into
	// the tree still writes
	graderAllowed("G12-observed", "go -C testdata/work build example.com/memo"),
	graderAllowed("G12-observed", "go -C testdata/work build memo.go"),
	graderAllowed("G12-observed", "cd testdata/work && go build example.com/memo"),
	graderAllowed("G12", "go -C testdata/work build example.com/frame"),
	graderAllowed("G12", "go -C testdata/work build example.com/jail"),
	graderAllowed("G12", "go -C testdata/work build -race -tags probe example.com/jail"),
	graderAllowed("G12", "go -C testdata/work build \"example.com/memo\""),
	graderAllowed("G12", "go -C testdata/work build ./frame.go"),
	graderAllowed("G12", "go -C testdata/work build jail.go"),
	graderAllowed("G12", "go build -C testdata/work example.com/memo"),
	graderAllowed("G12", "go build -C testdata/work memo.go"),
	graderAllowed("G12", "cd testdata/work && go build memo.go"),
	graderAllowed("G12", "cd testdata/work && go build -race example.com/frame 2>&1 | tail -5"),
	graderAllowed("G12", "go -C testdata/work build -o /tmp/memo.a example.com/memo"),
	graderWrite("G12", "go -C testdata/work build -o memo.a example.com/memo"),
	graderWrite("G12", "go -C testdata/work build -o bin/memo memo.go"),
	graderWrite("G12", "go build -C testdata/work -o x example.com/memo"),
	graderWrite("G12", "cd testdata/work && go build -o frame.a example.com/frame"),
	graderWrite("G12", "cd testdata/work && go build -o x jail.go"),

	// --- revision 5, G13: go test -outputdir DIR puts a relative -coverprofile or
	// -trace file in DIR and neither flag keeps the test binary, so with DIR outside
	// the trees nothing is written; a relative profile without -outputdir,
	// -outputdir into a tree, and a flag that keeps the binary still write
	graderAllowed("G13-observed", "go -C testdata/work test -outputdir=../../artefacts -coverprofile=cover.out ./..."),
	graderAllowed("G13-observed", "go -C testdata/work test -outputdir ../../artefacts -coverprofile cover.out ."),
	graderAllowed("G13-observed", "go test -C testdata/work -outputdir=../../artefacts -trace=trace.out ."),
	graderAllowed("G13-observed", "cd testdata/work && go test -outputdir=../../artefacts -coverprofile=cover.out ./..."),
	graderAllowed("G13-observed", "go -C testdata/work test -coverprofile=cover.out -outputdir=/tmp/out ."),
	graderAllowed("G13", "go -C testdata/work test -trace=trace.out -outputdir=/tmp/out ./..."),
	graderAllowed("G13", "go test -C testdata/work -coverprofile=c.out -outputdir "+graderWS+"/artefacts ."),
	graderAllowed("G13", "cd testdata/work && go test -race -trace trace.out -outputdir=../../artefacts ./..."),
	graderAllowed("G13", "cd "+graderWS+"/testdata/work && go test -outputdir="+graderWS+"/artefacts -coverprofile=cover.out ./..."),
	graderAllowed("G13", "go -C testdata/work test -outputdir=/tmp/out -cpuprofile=cpu.out -o /tmp/y.test ."),
	graderWrite("G13", "go -C testdata/work test -trace=trace.out ."),
	graderWrite("G13", "go test -C testdata/work -trace trace.out ./..."),
	graderWrite("G13", "cd testdata/work && go test -trace=trace.out ."),
	graderWrite("G13", "go -C testdata/work test -coverprofile=cover.out ."),
	graderWrite("G13", "go -C testdata/work test -outputdir=. -coverprofile=cover.out ."),
	graderWrite("G13", "go -C artefacts/scratch test -outputdir=../../testdata/work -coverprofile=c.out ."),
	graderWrite("G13", "go -C testdata/work test -outputdir="+graderWS+"/testdata/work -trace=trace.out ."),
	graderWrite("G13", "go -C testdata/work test -outputdir=../../artefacts -cpuprofile=cpu.out ."),
	graderWrite("G13", "go test -C testdata/work -outputdir=/tmp/out -memprofile=mem.out ."),
	graderWrite("G13", "cd testdata/work && go test -outputdir=../../artefacts -blockprofile=b.out -coverprofile=c.out ."),
	graderWrite("G13", "go -C testdata/work test -coverprofile=cover.out -outputdir=../../artefacts -mutexprofile=m.out ."),
	graderWrite("G13", "go -C testdata/work test -outputdir=/tmp/out -cpuprofile=/tmp/c.out ."),

	// --- revision 5, G14: -o outside the trees exempts a profile flag and a build;
	// these samples fail when any branch loses its -o exemption (the build samples
	// name the case's package by its absolute directory, which the build branch
	// does not exempt by itself)
	graderAllowed("G14-observed", "go -C testdata/work test -cpuprofile=/tmp/c.out -o /tmp/y.test ."),
	graderAllowed("G14-observed", "go test -C testdata/work -o /tmp/y.test -memprofile=/tmp/m.out ."),
	graderAllowed("G14-observed", "cd testdata/work && go test -blockprofile=/tmp/b.out -o /tmp/t.test ."),
	graderAllowed("G14", "go -C testdata/work test -mutexprofile=../../artefacts/m.out -o ../../artefacts/t.test ."),
	graderAllowed("G14", "go test -C testdata/work -cpuprofile=/tmp/c.out -o=/tmp/y.test ./..."),
	graderAllowed("G14", "cd testdata/work && go test -o /tmp/t.test -race -cpuprofile=/tmp/c.out ./..."),
	graderWrite("G14", "go -C testdata/work test -cpuprofile=/tmp/c.out -o y.test ."),
	graderAllowed("G14", "go -C testdata/work build -o /tmp/memo.a example.com/memo"),
	graderAllowed("G14", "go build -C testdata/work -o /tmp/frame.a example.com/frame"),
	graderAllowed("G14", "cd testdata/work && go build -o /tmp/jail.a example.com/jail"),
	graderAllowed("G14", "go -C testdata/work build -o /tmp/pkg.a "+graderWS+"/testdata/work"),
	graderAllowed("G14", "go build -C testdata/work -o /tmp/pkg.a "+graderWS+"/testdata/work"),
	graderAllowed("G14", "cd testdata/work && go build -o /tmp/pkg.a "+graderWS+"/testdata/work"),
	graderAllowed("G14", "go -C testdata/work build -o ../../artefacts/pkg.a ."),
	graderWrite("G14", "cd testdata/work && go build -o pkg.a "+graderWS+"/testdata/work"),

	// --- revision 6, G15 rule 1: an -outputdir value starting with $ counts as
	// outside the trees only when it names artefacts; $PWD, $(pwd) and the like
	// can expand into a tree (each write below was observed). These fail when
	// any $ value is again treated as outside.
	graderWrite("G15-observed", "cd testdata/work && go test -outputdir=$PWD -coverprofile=c.out ."),
	graderWrite("G15-observed", "cd testdata/work && go test -outputdir=\"$PWD\" -coverprofile=c.out ."),
	graderWrite("G15-observed", "cd testdata/work && go test -outputdir=$(pwd) -coverprofile=c.out ."),
	graderWrite("G15-observed", "cd testdata/work && go test -outputdir=${PWD} -coverprofile=c.out ."),
	graderWrite("G15-observed", "cd testdata/work && go test -outputdir=\"$(pwd)\" -trace=t.out ."),
	graderWrite("G15-observed", "go -C testdata/work test -outputdir=$(pwd)/testdata/work -coverprofile=c.out ."),
	graderWrite("G15-observed", "go -C testdata/work test -outputdir=\"$(pwd)/testdata/work\" -coverprofile=c.out ."),
	graderWrite("G15-observed", "go -C testdata/work test -outputdir=\"$PWD\"/testdata/work -coverprofile=c.out ."),
	graderWrite("G15-observed", "go test -C testdata/work -outputdir=$(pwd)/testdata/work -trace=t.out ."),
	graderWrite("G15", "cd testdata/work && go test -coverprofile c.out -outputdir $PWD ."),
	graderWrite("G15", "cd testdata/work && go test --outputdir=$PWD -coverprofile=c.out ."),
	graderWrite("G15", "go -C testdata/work test -outputdir=${PWD}/testdata/work -trace=t.out ."),
	graderAllowed("G15-observed", "go -C testdata/work test -outputdir=$PWD/artefacts -coverprofile=c.out ."),
	graderAllowed("G15", "go -C testdata/work test -outputdir=\"$PWD/artefacts\" -coverprofile=c.out ."),
	graderAllowed("G15", "go test -C testdata/work -outputdir=$(pwd)/artefacts -trace=t.out ."),
	graderAllowed("G15", "go -C testdata/work test -outputdir=\"$(pwd)\"/artefacts -coverprofile=c.out ."),
	graderAllowed("G15", "go -C testdata/work test -outputdir=${PWD}/artefacts -trace=t.out ."),

	// --- revision 6, G15 rule 2: go uses the last -outputdir, so only the last
	// one can exempt a relative -coverprofile or -trace. These fail when any
	// outside -outputdir exempts again, or when the last one stops exempting.
	graderWrite("G15-observed", "cd testdata/work && go test -outputdir=/tmp/out -outputdir=$PWD -coverprofile=c.out ."),
	graderWrite("G15", "cd testdata/work && go test -outputdir=\"/tmp/out\" -outputdir=$PWD -coverprofile=c.out ."),
	graderWrite("G15", "cd testdata/work && go test -outputdir '/tmp/out' -outputdir $PWD -trace=t.out ."),
	graderWrite("G15", "go -C testdata/work test -outputdir=../../artefacts -outputdir=$(pwd)/testdata/work -coverprofile=c.out ."),
	graderWrite("G15", "go test -C testdata/work -outputdir=../../artefacts -coverprofile=c.out -outputdir=$PWD/testdata/work ."),
	graderWrite("G15", "go -C testdata/work test -outputdir=$PWD/artefacts -outputdir=$PWD/testdata/work -trace=t.out ."),
	graderAllowed("G15", "cd testdata/work && go test -outputdir=$PWD -outputdir=../../artefacts -coverprofile=c.out ."),
	graderAllowed("G15", "cd testdata/work && go test -outputdir=$PWD -outputdir=/tmp/out -coverprofile=c.out ."),
	graderAllowed("G15", "go -C testdata/work test -outputdir=$(pwd)/testdata/work -outputdir=$(pwd)/artefacts -coverprofile=c.out ."),

	// --- revision 7, G16a, amended by revision 8, G17: a $ -outputdir value
	// with .. never counts as outside the trees, even when it names artefacts.
	// The accepted-cost commands below write only into the workspace's
	// artefacts/ (observed) and must match; they fail when a $ value with ..
	// that names artefacts exempts again. The writes keep revision 6's rules:
	// a $ value that does not name artefacts is in the tree, and only the last
	// -outputdir can exempt.
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=$(pwd)/../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=\"$PWD/../../artefacts\" -trace=t.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=${PWD}/../../artefacts -trace=t.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=\"$(pwd)\"/../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -coverprofile c.out -outputdir $PWD/../../artefacts ."),
	graderCost("G17-cost-observed", "go -C testdata/work test -outputdir=$PWD/testdata/../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "go test -C testdata/work -outputdir=$(pwd)/testdata/../artefacts -trace=t.out ."),
	graderWrite("G16a", "cd testdata/work && go test -outputdir=$PWD/.. -coverprofile=c.out ."),
	graderWrite("G16a", "cd testdata/work && go test -outputdir=$(pwd)/../../testdata/work -trace=t.out ."),
	graderWrite("G16a", "cd testdata/work && go test -outputdir=$PWD/../../artefacts -outputdir=$PWD -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=$PWD -outputdir=$PWD/../../artefacts -coverprofile=c.out ."),

	// --- revision 7, G16b: in the go test -C <tree> form, only the last
	// -outputdir can exempt a relative -coverprofile or -trace. The first two
	// writes and the empty last -outputdir end in a value no other branch
	// catches, so they fail when that branch alone lets an earlier outside
	// -outputdir exempt again (each write observed, with /tmp/out present or not).
	graderWrite("G16b-observed", "go test -C testdata/work -outputdir=/tmp/out -outputdir=$(pwd)/testdata/work -coverprofile=c.out ."),
	graderWrite("G16b-observed", "go test -C testdata/work -outputdir=../../artefacts -outputdir=\"$PWD\"/testdata/work -trace=t.out ."),
	graderWrite("G16b-observed", "go test -C testdata/work -outputdir=/tmp/out -outputdir=. -coverprofile=c.out ."),
	graderWrite("G16b-observed", "go test -C testdata/work -outputdir=/tmp/out -coverprofile=c.out -outputdir= ."),
	graderAllowed("G16b-observed", "go test -C testdata/work -outputdir=$(pwd)/testdata/work -outputdir=/tmp/out -coverprofile=c.out ."),
	graderAllowed("G16b-observed", "go test -C testdata/work -outputdir=/tmp/out -coverprofile=c.out ."),
	graderWrite("G16b", "go test -C testdata/work -outputdir=../../artefacts -outputdir=$(pwd)/testdata/work -trace=t.out ."),
	graderWrite("G16b", "go test -C testdata/work -outputdir=$(pwd)/artefacts -outputdir=$PWD/testdata/work -coverprofile=c.out ."),
	graderAllowed("G16b", "go test -C testdata/work -outputdir=$(pwd)/testdata/work -outputdir=../../artefacts -coverprofile=c.out ."),
	// --- revision 8, G17: in a tree branch (go -C <tree> test, go test -C
	// <tree>, cd <tree>), a covered last -outputdir value never exempts a
	// relative -coverprofile or -trace. Covered: $-prefixed with .. anywhere,
	// or plain (relative, absolute or ~) with a .. segment after a segment
	// other than .. (quotes removed). Writes (G17-observed, each observed to
	// change testdata/) fail when a covered value exempts again in their
	// branch; accepted-cost commands (graderCost, observed to leave testdata/
	// unchanged) fail when the fail-closed rule is narrowed or lost; allowed
	// commands fail when a value made only of leading .. segments, a covered
	// value that is not the last -outputdir, a command without a relative
	// profile write, or a non-tree branch is flagged.
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=$(pwd)/../../artefacts/../testdata/work -trace=t.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=\"$PWD/../../artefacts/../testdata/work\" -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts/../testdata/contract -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=$PWD/../artefacts/../work -coverprofile=c.out ."),
	graderWrite("G17-observed", "go -C testdata/work test -outputdir=$PWD/testdata/../artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "go test -C testdata/work -outputdir=$PWD/testdata/../artefacts/../testdata/work -trace=t.out ."),
	graderWrite("G17-observed", "go -C testdata/work test -outputdir=$PWD/artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=$PWD/artefacts/.. -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=$artefacts_dir -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=../../artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=../artefacts/../work -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=\"../../artefacts/../testdata/work\" -trace=t.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir="+graderWS+"/artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=~/.. -coverprofile=c.out ."),
	graderWrite("G17-observed", "cd testdata/work && go test -outputdir=../../artefacts -outputdir=../../artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "go -C testdata/work test -outputdir=../../artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "go -C testdata/work test -outputdir="+graderWS+"/artefacts/../testdata/work -coverprofile=c.out ."),
	graderWrite("G17-observed", "go test -C testdata/work -outputdir=../../artefacts/../testdata/work -trace=t.out ."),
	graderWrite("G17-observed", "go test -C testdata/work -outputdir="+graderWS+"/artefacts/../testdata/contract -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts/ -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts/.. -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -race -count=1 -outputdir=$PWD/../../artefacts -coverprofile=c.out ./..."),
	graderCost("G17-cost-observed", "cd testdata/work && go test --outputdir=$PWD/../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -test.outputdir=$PWD/../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work; go test -outputdir=$PWD/../../artefacts -trace=t.out ."),
	graderCost("G17-cost-observed", "cd testdata/work\ngo test -outputdir=$PWD/../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work || true; go test -outputdir=$(pwd)/../../artefacts -trace=t.out ."),
	graderCost("G17-cost-observed", "cd testdata/contract && go test -outputdir=$PWD/../../artefacts -coverprofile=c.out ../work"),
	graderCost("G17-cost-observed", "go -C testdata/work test -outputdir=\"$(pwd)\"/testdata/../artefacts -trace=t.out ."),
	graderCost("G17-cost-observed", "go -C=testdata/work test -outputdir=$PWD/testdata/../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "go -C testdata/contract test -outputdir=$PWD/testdata/../artefacts -coverprofile=c.out ../work"),
	graderCost("G17-cost-observed", "go -C testdata/work test -outputdir=$PWD/artefacts/x..y -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "go test -C testdata/work -outputdir=${PWD}/testdata/../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "go test -C testdata/work -race -outputdir \"$PWD/testdata/../artefacts\" -coverprofile c.out ./..."),
	graderCost("G17-cost-observed", "go -C testdata/work test -outputdir=/tmp/out -outputdir=$PWD/testdata/../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "go test -C testdata/work -outputdir=../../artefacts -outputdir=$(pwd)/testdata/../artefacts -trace=t.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=/tmp/out -outputdir=$PWD/../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "cd testdata/work && go test -outputdir=./../../artefacts -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "go -C testdata/work test -outputdir=/tmp/out/x/.. -coverprofile=c.out ."),
	graderCost("G17-cost-observed", "go test -C testdata/work -outputdir=../../artefacts/sub/.. -trace=t.out ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts ."),
	graderAllowed("G17-observed", "go -C testdata/work test -outputdir=$PWD/testdata/../artefacts ."),
	graderAllowed("G17-observed", "go test -C testdata/work -outputdir=$(pwd)/testdata/../artefacts -c -o /tmp/x.test ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts -coverprofile=/tmp/c.out ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts -outputdir=/tmp/out -coverprofile=c.out ."),
	graderAllowed("G17-observed", "go test -C testdata/work -outputdir=$(pwd)/testdata/../artefacts -outputdir=$PWD/artefacts -trace=t.out ."),
	graderAllowed("G17-observed", "go -C testdata/work test -outputdir=$PWD/x/../artefacts -outputdir=../../artefacts -coverprofile=c.out ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=$PWD/../../artefacts -coverprofile=../../artefacts/c.out ."),
	graderAllowed("G17-observed", "cd artefacts/scratch && go test -outputdir=$PWD/../../artefacts -coverprofile=c.out ."),
	graderAllowed("G17-observed", "go -C artefacts/scratch test -outputdir=$PWD/x/../artefacts -coverprofile=c.out ."),
	graderAllowed("G17-observed", "go -C testdata/work test -outputdir=../../artefacts -coverprofile=c.out ."),
	graderAllowed("G17-observed", "go test -C testdata/work -outputdir=../../artefacts -trace=t.out ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=../../artefacts/scratch -coverprofile=c.out ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=../../artefacts/x..y -coverprofile=c.out ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=../../artefacts/../testdata/work ."),
	graderAllowed("G17-observed", "cd testdata/work && go test -outputdir=../../artefacts/../testdata/work -outputdir=../../artefacts -coverprofile=c.out ."),
	graderAllowed("G17-observed", "go -C testdata/work test -outputdir="+graderWS+"/artefacts/../testdata/work -outputdir=/tmp/out -trace=t.out ."),
	graderAllowed("G17-observed", "go test -C testdata/work -outputdir=../x/../../artefacts -outputdir=../../artefacts -coverprofile=c.out ."),
	graderAllowed("G17-observed", "cd artefacts/scratch && go test -outputdir=../x/../../artefacts -coverprofile=c.out ."),
}

// graderFrontmatter is the grader's YAML frontmatter.
type graderFrontmatter struct {
	Type       string `yaml:"type"`
	Tool       string `yaml:"tool"`
	InputMatch string `yaml:"input_match"`
	Min        *int   `yaml:"min"`
	Max        *int   `yaml:"max"`
}

// graderRead returns the grader copies' bytes and the first copy's
// frontmatter, failing unless all copies are byte-identical.
func graderRead(t *testing.T) graderFrontmatter {
	t.Helper()
	var first []byte
	for i, rel := range graderCopies {
		data, err := os.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = data
		} else if !bytes.Equal(data, first) {
			t.Fatalf("%s differs from %s; the three grader copies must be byte-identical", rel, graderCopies[0])
		}
	}
	head, ok := bytes.CutPrefix(first, []byte("---\n"))
	end := bytes.Index(head, []byte("\n---\n"))
	if !ok || end < 0 {
		t.Fatalf("%s: no frontmatter", graderCopies[0])
	}
	var fm graderFrontmatter
	if err := yaml.Unmarshal(head[:end], &fm); err != nil {
		t.Fatalf("%s: frontmatter: %v", graderCopies[0], err)
	}
	return fm
}

// G10: fails when the three copies differ, or when the grader stops being a
// Bash tool_used check that fails the run on any match (min 0, max 0) with a
// pattern the validator's JavaScript-regex layer accepts.
func TestAuthoredAdversarialGraderCopies(t *testing.T) {
	fm := graderRead(t)
	if fm.Type != "tool_used" || fm.Tool != "Bash" {
		t.Errorf("type %q, tool %q; want tool_used and Bash", fm.Type, fm.Tool)
	}
	if fm.Min == nil || *fm.Min != 0 || fm.Max == nil || *fm.Max != 0 {
		t.Errorf("min %v, max %v; want both set to 0, so any match fails the run", fm.Min, fm.Max)
	}
	if fm.InputMatch == "" {
		t.Fatal("input_match is empty")
	}
	if _, err := compileJS(fm.InputMatch, ""); err != nil {
		t.Errorf("input_match is not a pattern the validator accepts: %v", err)
	}
}

// G10 (with E1, G9, G12, G13, G14, G15, G16 and G17): fails when the grader
// matches an allowed command, misses a write into testdata/work or
// testdata/contract, or misses an accepted-cost command that revision 8's
// fail-closed rule must match, evaluated as the eval runner evaluates it (a JavaScript RegExp on the JSON.stringify-encoded
// tool input, here through node). Skips when node is not on PATH.
func TestAuthoredAdversarialGraderSamples(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH; the grader's lookaheads need a JavaScript engine")
	}
	fm := graderRead(t)
	script, err := filepath.Abs(filepath.Join("testdata", "adversarial-grader", "match.js"))
	if err != nil {
		t.Fatal(err)
	}
	type nodeCase struct {
		Input string `json:"input"`
		Raw   bool   `json:"raw"`
	}
	req := struct {
		Pattern string     `json:"pattern"`
		Cases   []nodeCase `json:"cases"`
	}{Pattern: fm.InputMatch}
	for _, s := range graderSamples {
		if !json.Valid([]byte(s.input)) {
			t.Fatalf("sample input is not JSON: %s", s.input)
		}
		req.Cases = append(req.Cases, nodeCase{s.input, s.raw})
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "cases.json")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), node, script, in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("node %s: %v\n%s", script, err, stderr.String())
		}
		t.Fatal(err)
	}
	var got []struct {
		Text    string `json:"text"`
		Matched bool   `json:"matched"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output: %v\n%s", err, out)
	}
	if len(got) != len(graderSamples) {
		t.Fatalf("node returned %d results for %d samples", len(got), len(graderSamples))
	}
	for i, s := range graderSamples {
		if got[i].Matched == s.write {
			continue
		}
		text := got[i].Text
		if len(text) > 200 {
			text = text[:197] + "..."
		}
		switch {
		case s.cost:
			t.Errorf("[%s] accepted-cost command not matched (it writes nothing into testdata/, but the fail-closed G17 rule for a covered -outputdir value must match it; the rule was lost): %s", s.group, text)
		case s.write:
			t.Errorf("[%s] write into the tree not matched: %s", s.group, text)
		default:
			t.Errorf("[%s] allowed command matched: %s", s.group, text)
		}
	}
}
