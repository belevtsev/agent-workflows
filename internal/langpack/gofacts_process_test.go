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

// gfWatchFIFO makes a FIFO and reads it in the background with blocking
// calls, which work on every Unix (macOS cannot poll a FIFO, so it has no
// read deadline): the first five bytes arrive on held, and ended is closed at
// end of file, once every writer has exited. Cleanup releases a reader still
// waiting for a writer to open it.
func gfWatchFIFO(t *testing.T) (string, <-chan string, <-chan struct{}) {
	t.Helper()
	path := filepath.Join(gfDir(t), "held")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	held, ended := make(chan string, 1), make(chan struct{})
	go func() {
		defer close(ended)
		file, err := os.Open(path)
		if err != nil {
			held <- err.Error()
			return
		}
		defer file.Close()
		first := make([]byte, 5)
		n, _ := io.ReadFull(file, first)
		held <- string(first[:n])
		_, _ = io.Copy(io.Discard, file)
	}()
	t.Cleanup(func() {
		if writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			writer.Close()
		}
	})
	return path, held, ended
}

// Oracle: when LoadGoFacts returns, a child that the graph's go started in the
// background is no longer alive, whether go exited on its own or was stopped
// at the end of a 1 s budget. go opens a FIFO, writes "held", starts the
// child, which inherits the write end, and closes its own copy. The FIFO's
// reader reaches end of file only once every holder has exited, so "held"
// then end of file within 10 s means the child died; it would live for 60 s.
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
			fifo, held, ended := gfWatchFIFO(t)
			pids := gfPids(t)
			script := gfProbeOK + "exec 3>'" + fifo + "'\necho held >&3\n/bin/sleep 60 </dev/null >/dev/null 2>&1 &\n" +
				"echo $! >> '" + pids + "'\nexec 3>&-\n" + r.end
			dir := gfModule(t, map[string]string{"go.mod": "module example.com/t\n\ngo 1.21\n", "a/a.go": "package a\n"})
			gfLoad(t, r.ctx(t), dir, gfEnv("PATH="+gfFake(t, script)), src)
			select {
			case got := <-held:
				if got != "held\n" {
					t.Fatalf("the FIFO gave %q, want %q: go did not start its child", got, "held\n")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("go never wrote to the FIFO")
			}
			select {
			case <-ended:
			case <-time.After(10 * time.Second):
				t.Error("go's background child still held the FIFO 10 s after LoadGoFacts returned")
			}
		})
	}
}
