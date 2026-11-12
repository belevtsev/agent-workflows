//go:build linux || darwin

package langpack_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// gfHeldFIFO makes a FIFO and opens its read end without blocking, so a fake
// go can open the write end at once.
func gfHeldFIFO(t *testing.T) (string, *os.File) {
	t.Helper()
	path := filepath.Join(gfDir(t), "held")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	return path, reader
}

// Oracle: when LoadGoFacts returns, a child that the graph's go started in the
// background is no longer alive, whether go exited on its own or was stopped
// at the end of a 1 s budget. go opens a FIFO, writes "held", starts the
// child, which inherits the write end, and closes its own copy. A read of the
// FIFO reaches end of file only once every holder has exited, so "held" then
// end of file within 10 s means the child died; it would live for 60 s.
func TestLoadGoFactsLeavesNoChildOfGoRunning(t *testing.T) {
	src := langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}}
	oneSecond := func(t *testing.T) context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		t.Cleanup(cancel)
		return ctx
	}
	for _, r := range []struct {
		name string
		end  string // how the graph's go ends after starting the child
		ctx  func(t *testing.T) context.Context
	}{
		{"go exits 0", "exit 0\n", gfCtx},
		{"go hangs past a 1 s budget", "exec /bin/sleep 60\n", oneSecond},
	} {
		t.Run(r.name, func(t *testing.T) {
			fifo, reader := gfHeldFIFO(t)
			pids := gfPids(t)
			script := gfProbeOK + "exec 3>'" + fifo + "'\necho held >&3\n/bin/sleep 60 </dev/null >/dev/null 2>&1 &\n" +
				"echo $! >> '" + pids + "'\nexec 3>&-\n" + r.end
			dir := gfModule(t, map[string]string{"go.mod": "module example.com/t\n\ngo 1.21\n", "a/a.go": "package a\n"})
			gfLoad(t, r.ctx(t), dir, gfEnv("PATH="+gfFake(t, script)), src)
			if err := reader.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			held, err := io.ReadAll(reader)
			if string(held) != "held\n" {
				t.Fatalf("the FIFO gave %q (%v), want %q: go did not start its child", held, err, "held\n")
			}
			if err != nil {
				t.Errorf("go's background child still held the FIFO 10 s after LoadGoFacts returned: %v", err)
			}
		})
	}
}
