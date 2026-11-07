package skillcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRetrySkillFullJitterBounds(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../skills/retry-amplification/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(data), "func fullJitter(")
	if start < 0 {
		t.Fatal("fullJitter example missing")
	}
	end := strings.Index(string(data[start:]), "\n```")
	if end < 0 {
		t.Fatal("fullJitter code fence missing")
	}
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "go.mod"), []byte("module jitterfixture\n\ngo 1.27.1\n"))
	writeFixture(t, filepath.Join(root, "jitter.go"), []byte("package jitterfixture\nimport (\"math/rand/v2\"; \"time\")\n"+string(data[start:start+end])+"\n"))
	writeFixture(t, filepath.Join(root, "jitter_test.go"), []byte(`package jitterfixture
import ("testing"; "time")
func TestBounds(t *testing.T) {
 maxDuration := time.Duration(1<<63-1)
 for _, tc := range []struct { name string; attempt int; base, ceiling, bound time.Duration }{
  {"initial", 0, 10*time.Second, time.Minute, 10*time.Second},
  {"double", 1, 10*time.Second, time.Minute, 20*time.Second},
  {"uncapped", 2, 10*time.Second, time.Minute, 40*time.Second},
  {"capped", 3, 10*time.Second, time.Minute, time.Minute},
  {"review-overflow", 30, 10*time.Second, time.Minute, time.Minute},
  {"large-base", 1, maxDuration/2+1, maxDuration, maxDuration},
  {"huge-attempt", int(^uint(0)>>1), 1, maxDuration, maxDuration},
  {"base-above-ceiling", 0, time.Minute, time.Second, time.Second},
  {"one-nanosecond", 20, 1, 1, 1},
 } {
  t.Run(tc.name, func(t *testing.T) {
   for range 32 {
    got := fullJitter(tc.attempt, tc.base, tc.ceiling)
    if got < 0 || got >= tc.bound { t.Fatalf("delay %v outside [0,%v)", got, tc.bound) }
   }
  })
 }
}
func TestInvalidConfiguration(t *testing.T) {
 for _, tc := range []struct { attempt int; base, ceiling time.Duration }{
  {-1, 1, 1}, {0, 0, 1}, {0, 1, 0}, {0, -1, 1}, {0, 1, -1},
 } {
  func() {
   defer func() { if recover() == nil { t.Errorf("invalid parameters accepted: %+v", tc) } }()
   fullJitter(tc.attempt, tc.base, tc.ceiling)
  }()
 }
}
`))
	command := exec.CommandContext(t.Context(), filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-count=1", "-timeout=10s", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off", "GOENV=off", "GOFLAGS=", "GOTOOLCHAIN=local", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("shipped fullJitter example: %v\n%s", err, output)
	}
}
