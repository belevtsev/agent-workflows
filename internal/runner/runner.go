// Package runner executes one check and records how it ended.
package runner

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/proc"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// controlCExit is the exit status of a Windows program that the console
// ended, as Ctrl+C, Ctrl+Break and closing the console do.
const controlCExit = 0xC000013A

// Job is one check to execute on one snapshot.
type Job struct {
	Check decl.Check
	Tree  string // the snapshot taken immediately before
	Key   string // the check's content key for that snapshot
	// Program is the file to start when the caller looked the program up
	// itself; empty starts the first word of the check's command.
	Program string
	// Out and Err receive the program's output as it is produced.
	Out, Err io.Writer
}

// Result is a finished run.
type Result struct {
	Record   store.Record
	Warnings []string
}

// Execute runs the check's program from the repository root, without a
// shell, with the caller's environment and no input, and records the run for
// the snapshot the job names. The outcome is passed only when the program
// exits 0 and the snapshot after it equals the snapshot before it.
func Execute(ctx context.Context, repo *gitx.Repo, st *store.Store, job Job) (Result, error) {
	start := time.Now()
	runID, err := store.NewRunID(start)
	if err != nil {
		return Result{}, err
	}
	run, err := st.Begin(ctx, store.Record{
		RunID: runID, ID: job.Check.ID, Run: job.Check.Run, Paths: job.Check.Paths,
		Tree: job.Tree, ContentKey: job.Key, StartedAt: start.UTC().Truncate(time.Second),
	})
	if err != nil {
		return Result{}, fmt.Errorf("the run of `%s` cannot be recorded in %s: %w", job.Check.ID, st.Dir, err)
	}
	// A cancelled run is still recorded, so the context of the record is not
	// the one that was cancelled.
	finish := func(end ending) (store.Record, error) {
		return run.Finish(context.WithoutCancel(ctx), func(r *store.Record) {
			now := time.Now().UTC().Truncate(time.Second)
			r.Outcome, r.ExitStatus, r.EndedAt = end.outcome, end.exit, &now
			r.Differ, r.DifferMore, r.Detail = end.differ, end.more, end.detail
		})
	}
	logFile, err := os.OpenFile(st.LogPath(runID), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		_, _ = finish(ending{outcome: store.Interrupted, detail: "the log could not be written: " + err.Error()})
		return Result{}, fmt.Errorf("the log of `%s` cannot be written: %w", job.Check.ID, err)
	}
	log := newLog(logFile)
	var warnings []string
	ended := proc.Run(ctx, proc.Spec{
		Path: cmp.Or(job.Program, job.Check.Run[0]), Args: job.Check.Run[1:], Dir: repo.Root, Env: os.Environ(),
		Stdout: &tee{log: log, terminal: job.Out}, Stderr: &tee{log: log, terminal: job.Err},
		Timeout: job.Check.Timeout,
	})
	if errors.Is(ended.Err, exec.ErrWaitDelay) {
		warnings = append(warnings, fmt.Sprintf("`%s`: a process the check started was left behind and held its output open; kitsu stopped it", job.Check.ID))
	}
	if ended.Cleanup != nil {
		warnings = append(warnings, fmt.Sprintf("`%s`: a process the check started could not be stopped: %v", job.Check.ID, ended.Cleanup))
	}
	end := classify(ctx, repo, st, job, ended)
	if end.detail != "" {
		_, _ = log.Write([]byte("\nkitsu: " + end.detail + "\n"))
	}
	if err := log.Close(); err != nil {
		warnings = append(warnings, fmt.Sprintf("`%s`: the log is incomplete: %v", job.Check.ID, err))
	}
	record, err := finish(end)
	return Result{Record: record, Warnings: warnings}, err
}

// ending is how a run ended, before it is recorded.
type ending struct {
	outcome string
	exit    *int
	differ  []string
	more    int
	detail  string
}

// classify says how a run ended. A program that ended with a failure while
// the run was being cancelled was interrupted, whichever of the two kitsu saw
// first: on Windows the console sends its signal to the program as well.
func classify(ctx context.Context, repo *gitx.Repo, st *store.Store, job Job, ended proc.Result) ending {
	switch {
	case ended.Stopped && ended.Cancelled, ended.Code != 0 && errors.Is(ctx.Err(), context.Canceled), uint32(ended.Code) == controlCExit: //nolint:gosec // G115: compared as the 32 bits Windows gives.
		return ending{outcome: store.Interrupted}
	case ended.Stopped:
		return ending{outcome: store.TimedOut, detail: fmt.Sprintf("the check did not end within its timeout of %s", job.Check.Timeout)}
	case ended.Code < 0:
		// The program started and something outside kitsu ended it, or it
		// never started.
		var exited *exec.ExitError
		detail := fmt.Sprintf("the program `%s` could not be started", job.Check.Run[0])
		if errors.As(ended.Err, &exited) {
			detail = fmt.Sprintf("the program `%s` did not exit on its own", job.Check.Run[0])
		}
		if ended.Err != nil {
			detail += ": " + ended.Err.Error()
		}
		return ending{outcome: store.Failed, detail: detail}
	case ended.Code != 0:
		return ending{outcome: store.Failed, exit: &ended.Code}
	}
	exit := 0
	after, err := repo.Snapshot(ctx, st.Tmp())
	if err != nil {
		if ctx.Err() != nil {
			return ending{outcome: store.Interrupted, exit: &exit}
		}
		return ending{outcome: store.SourceChanged, exit: &exit, detail: "after the run " + err.Error()}
	}
	if after == job.Tree {
		return ending{outcome: store.Passed, exit: &exit}
	}
	end := ending{outcome: store.SourceChanged, exit: &exit, detail: "the source changed during the run"}
	if end.differ, end.more, err = repo.DiffPaths(ctx, job.Tree, after); err != nil {
		return end
	}
	end.detail += ": " + decl.Plain(strings.Join(end.differ, ", "))
	if end.more > 0 {
		end.detail += fmt.Sprintf(" and %d more", end.more)
	}
	// A file the check itself wrote is the usual cause, and the next run
	// would pass only because the file is then already there.
	end.detail += "; when the check writes these files, list them in .gitignore or write them outside the working tree"
	return end
}

// tee sends the output of the program to the terminal and to the log. The
// two streams of a program share one log.
type tee struct {
	log      *runLog
	terminal io.Writer
}

func (t *tee) Write(p []byte) (int, error) {
	_, _ = t.log.Write(p)
	if t.terminal != nil {
		_, _ = t.terminal.Write(p)
	}
	return len(p), nil
}

// The log keeps the first headLimit bytes and the last tailLimit bytes.
const (
	headLimit = 1 << 20
	tailLimit = 9 << 20
)

// runLog writes the output of a run to its file as it arrives, up to the
// limit. Past the head it also keeps the newest bytes in memory, and when
// the limit is passed Close rewrites the file as the head, a mark, and the
// tail.
type runLog struct {
	mu      sync.Mutex
	file    *os.File
	written int64 // bytes that reached the file
	total   int64 // bytes received
	tail    []byte
	next    int // where the next byte goes once the tail is full
	err     error
}

func newLog(file *os.File) *runLog { return &runLog{file: file} }

func (l *runLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	before := l.total
	l.total += int64(n)
	if room := headLimit + tailLimit - l.written; room > 0 && l.err == nil {
		written, err := l.file.Write(p[:min(int64(n), room)])
		l.written += int64(written)
		l.err = err
	}
	if skip := headLimit - before; skip > 0 {
		p = p[min(int64(n), skip):]
	}
	l.keep(p)
	return n, nil
}

// keep adds bytes to the tail, which is a ring once it is full.
func (l *runLog) keep(p []byte) {
	if len(p) >= tailLimit {
		l.tail, l.next = append(l.tail[:0], p[len(p)-tailLimit:]...), 0
		return
	}
	if room := tailLimit - len(l.tail); room > 0 {
		take := min(room, len(p))
		l.tail = append(l.tail, p[:take]...)
		p = p[take:]
	}
	for len(p) > 0 {
		copied := copy(l.tail[l.next:], p)
		l.next = (l.next + copied) % tailLimit
		p = p[copied:]
	}
}

// Close finishes the file. When output was dropped, the file holds the
// first headLimit bytes, a line that says how much was dropped, and the last
// tailLimit bytes.
func (l *runLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.err
	if dropped := l.total - headLimit - int64(len(l.tail)); dropped > 0 && err == nil {
		mark := fmt.Sprintf("\n--- kitsu: %d bytes of output were dropped here ---\n", dropped)
		ordered := append(append([]byte(mark), l.tail[l.next:]...), l.tail[:l.next]...)
		if err = l.file.Truncate(headLimit); err == nil {
			_, err = l.file.WriteAt(ordered, headLimit)
		}
	}
	if closeErr := l.file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// BatchRefusal says why a check must not be started on Windows, or nothing.
// cmd.exe reads the command line of a batch file again, so an argument with
// a character outside a plain set could be read as a second command. Such a
// check is refused: kitsu never guesses a quoting. program is the file the
// system found for the check.
func BatchRefusal(program string, args []string) string {
	lower := strings.ToLower(program)
	if !strings.HasSuffix(lower, ".bat") && !strings.HasSuffix(lower, ".cmd") {
		return ""
	}
	var unsafe []string
	for _, arg := range args {
		plain := arg != ""
		for _, r := range arg {
			letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
			plain = plain && (letter || strings.ContainsRune(`_./:=+,@\-`, r))
		}
		if !plain {
			unsafe = append(unsafe, "`"+arg+"`")
		}
	}
	if len(unsafe) == 0 {
		return ""
	}
	return fmt.Sprintf("%s is a batch file, and cmd.exe could read these arguments as commands: %s; use only letters, digits and _ . / : = + , @ \\ - in the arguments of a batch file, or run a program that is not a batch file",
		program, strings.Join(unsafe, ", "))
}
