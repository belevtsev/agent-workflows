package gate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/decl"
	"github.com/belevtsev/agent-workflows/v3/internal/evidence"
	"github.com/belevtsev/agent-workflows/v3/internal/gitx"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
)

// maxPaths is how many differing paths a denial lists.
const maxPaths = 20

// Ruling is what a hook decided about one tree. A hook decides from the
// records alone: it executes no check and changes nothing.
type Ruling struct {
	Hook    string
	Ref     string // the ref a push updates
	Commit  string // the commit a push sends
	Tree    string
	Allowed bool
	// Verdict is set when the tree could be judged.
	Verdict *evidence.Verdict
	// Cause and Reason are set when something prevented a decision.
	Cause  string
	Reason string
	// Notes are printed whatever the ruling is.
	Notes []string
	// Differs says how the working tree differs from the denied content;
	// Elsewhere says that it does, so `cw run` here checks other content.
	Differs   string
	Elsewhere bool
}

// Gate judges for one repository.
type Gate struct {
	Repo  *gitx.Repo
	Store *store.Store
	Now   time.Time
}

// rule judges one tree.
func (g *Gate) rule(ctx context.Context, hook, tree string) Ruling {
	ruling := Ruling{Hook: hook, Tree: tree}
	verdict, err := evidence.Judge(ctx, g.Repo, g.Store, tree, g.Now, nil)
	var invalid *decl.Error
	var unreadable *evidence.StoreError
	switch {
	case err == nil:
		ruling.Verdict, ruling.Allowed = verdict, verdict.Verified
	case errors.Is(err, evidence.ErrNoDeclaration):
		ruling.Cause, ruling.Reason = store.CauseNoDeclaration, "the tree has no declaration: it holds no "+decl.FileName+" at its root"
	case errors.As(err, &invalid):
		ruling.Cause, ruling.Reason = store.CauseInvalidDeclaration, "the declaration is invalid:\n"+indent(invalid.Error())
	case errors.As(err, &unreadable):
		ruling.Cause, ruling.Reason = store.CauseStore, unreadable.Error()
	default:
		ruling.Cause, ruling.Reason = store.CauseInternal, err.Error()
	}
	return ruling
}

// Cannot is a ruling for a cause that prevented any judgement.
func Cannot(hook, cause, reason string) Ruling {
	return Ruling{Hook: hook, Cause: cause, Reason: reason}
}

// Commit judges the tree that a commit or a merge commit would record: the
// tree of the index, whichever way git built it.
func (g *Gate) Commit(ctx context.Context, hook string) Ruling {
	tree, err := g.Repo.IndexTree(ctx, g.Store.Tmp())
	if err != nil {
		return Cannot(hook, store.CauseTree, "the tree of the commit cannot be computed: "+err.Error())
	}
	ruling := g.rule(ctx, hook, tree)
	ruling.Notes = g.declarationNotice(ctx, ruling)
	if !ruling.Allowed && ruling.Cause != store.CauseStore && ruling.Cause != store.CauseInternal {
		g.compareWorktree(ctx, &ruling)
	}
	return ruling
}

// declarationNotice lists the checks that the tree of HEAD declares and the
// new tree lacks or declares differently. A check that is new gets no
// notice.
func (g *Gate) declarationNotice(ctx context.Context, ruling Ruling) []string {
	if ruling.Verdict == nil && ruling.Cause != store.CauseNoDeclaration {
		return nil
	}
	head, found, err := g.Repo.TreeOf(ctx, "HEAD")
	if err != nil || !found || head == ruling.Tree {
		return nil
	}
	before, err := evidence.Declaration(ctx, g.Repo, head)
	if err != nil {
		return nil
	}
	now := map[string]decl.Check{}
	if ruling.Verdict != nil {
		for _, c := range ruling.Verdict.Checks {
			now[c.ID] = c.Check
		}
	}
	var notes []string
	for _, old := range before.Checks {
		current, kept := now[old.ID]
		if !kept {
			notes = append(notes, fmt.Sprintf("notice: this commit changes the required checks: `%s` is no longer declared", old.ID))
			continue
		}
		var changed []string
		if !slices.Equal(old.Run, current.Run) {
			changed = append(changed, fmt.Sprintf("`run` was %q and becomes %q", old.Run, current.Run))
		}
		if !slices.Equal(old.Paths, current.Paths) {
			changed = append(changed, fmt.Sprintf("`paths` was %q and becomes %q", old.Paths, current.Paths))
		}
		if old.Timeout != current.Timeout {
			changed = append(changed, fmt.Sprintf("`timeout` was %s and becomes %s", old.Timeout, current.Timeout))
		}
		if len(changed) > 0 {
			notes = append(notes, fmt.Sprintf("notice: this commit changes the required checks: `%s`: %s", old.ID, strings.Join(changed, "; ")))
		}
	}
	return notes
}

// compareWorktree says how the denied commit relates to the working tree,
// which is what `cw run` checks. The snapshot is taken only for a denial.
func (g *Gate) compareWorktree(ctx context.Context, ruling *Ruling) {
	snapshot, err := g.Repo.Snapshot(ctx, g.Store.Tmp())
	if err != nil {
		ruling.Elsewhere = true
		ruling.Differs = "No snapshot of the working tree could be taken, so it is not known how it differs from this commit:\n" + indent(err.Error())
		return
	}
	if snapshot == ruling.Tree {
		return
	}
	ruling.Elsewhere = true
	changes, err := g.Repo.Diff(ctx, ruling.Tree, snapshot)
	if err != nil {
		ruling.Differs = "The working tree holds other content than this commit."
		return
	}
	var b strings.Builder
	b.WriteString("The working tree holds other content than this commit. Paths that differ:\n")
	for i, change := range changes {
		if i == maxPaths {
			fmt.Fprintf(&b, "  ... and %d more\n", len(changes)-maxPaths)
			break
		}
		fmt.Fprintf(&b, "  %s\n", change.Path)
	}
	if g.rule(ctx, ruling.Hook, snapshot).Allowed {
		b.WriteString("The working tree as a whole is verified; this commit is not, because it records other content.")
	} else {
		b.WriteString("The working tree as a whole is not verified either.")
	}
	ruling.Differs = b.String()
}

// zeroID says whether an object id is the all-zero id git uses for "none".
func zeroID(id string) bool { return strings.Trim(id, "0") == "" }

// Push judges each ref a push updates. git gives one line per ref on input:
// the local ref and its id, then the remote ref and its id.
func (g *Gate) Push(ctx context.Context, remote string, input io.Reader) []Ruling {
	const hook = "pre-push"
	var rulings []Ruling
	scanner := bufio.NewScanner(input)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 4 {
			continue
		}
		ruling := g.pushRef(ctx, remote, fields[1], fields[2], fields[3])
		ruling.Hook, ruling.Ref = hook, fields[2]
		rulings = append(rulings, ruling)
	}
	if err := scanner.Err(); err != nil {
		rulings = append(rulings, Cannot(hook, store.CauseInternal, "the refs of the push cannot be read: "+err.Error()))
	}
	return rulings
}

// pushRef judges one update. A deletion and an update that adds nothing to
// the remote are allowed; otherwise the tree of the new tip must be
// verified. Only the tip is judged, not the commits below it.
func (g *Gate) pushRef(ctx context.Context, remote, localID, remoteRef, remoteID string) Ruling {
	const hook = "pre-push"
	if zeroID(localID) {
		return Ruling{Allowed: true}
	}
	// An annotated tag is judged by the commit it names.
	commit, isCommit, err := g.Repo.CommitOf(ctx, localID)
	if err != nil {
		return Cannot(hook, store.CauseInternal, err.Error())
	}
	if isCommit {
		adds, err := g.addsSomething(ctx, remote, commit, remoteRef, remoteID)
		if err != nil {
			return Cannot(hook, store.CauseInternal, err.Error())
		}
		if !adds {
			return Ruling{Allowed: true, Commit: commit}
		}
	}
	tree, found, err := g.Repo.TreeOf(ctx, localID)
	if err != nil || !found {
		return Cannot(hook, store.CauseTree, "it names neither a commit nor a tree, so there is nothing to judge")
	}
	ruling := g.rule(ctx, hook, tree)
	ruling.Commit = commit
	if !ruling.Allowed && ruling.Verdict != nil {
		ruling.Elsewhere = !g.worktreeHolds(ctx, tree)
	}
	return ruling
}

// worktreeHolds says whether the working tree holds exactly this tree, so
// that `cw run` here would check it.
func (g *Gate) worktreeHolds(ctx context.Context, tree string) bool {
	if g.Repo.Root == "" {
		return false
	}
	snapshot, err := g.Repo.Snapshot(ctx, g.Store.Tmp())
	return err == nil && snapshot == tree
}

// addsSomething says whether pushing the commit to the ref gives the remote
// a commit it does not have. For a branch, the commit the branch points at
// on the remote decides; for any other ref, the remote-tracking branches.
func (g *Gate) addsSomething(ctx context.Context, remote, commit, remoteRef, remoteID string) (bool, error) {
	if strings.HasPrefix(remoteRef, "refs/heads/") {
		if zeroID(remoteID) {
			return true, nil
		}
		contained, err := g.Repo.Contains(ctx, commit, remoteID)
		return !contained, err
	}
	held, err := g.Repo.RemoteHolds(ctx, remote, commit)
	return !held, err
}

// Decision is the record of a ruling.
func (r Ruling) Decision(mode string, now time.Time) store.Decision {
	d := store.Decision{Time: now.UTC().Truncate(time.Second), Hook: r.Hook, Mode: mode, Tree: r.Tree, Ref: r.Ref, Outcome: "allowed"}
	if r.Allowed {
		return d
	}
	d.Outcome, d.Cause = "denied", r.Cause
	if r.Verdict != nil {
		d.Checks = map[string]string{}
		for _, c := range r.Verdict.Missing() {
			d.Checks[c.ID] = strings.ReplaceAll(c.State, " ", "_")
		}
	}
	return d
}

// subject names what the ruling is about.
func (r Ruling) subject() string {
	switch {
	case r.Ref != "":
		return "the push of " + r.Ref
	case r.Hook == "pre-merge-commit":
		return "this merge"
	case r.Hook == "pre-push":
		return "this push"
	}
	return "this commit"
}

// Because is the reason of a denial in a few words.
func (r Ruling) Because() string {
	switch r.Cause {
	case store.CauseNoDeclaration:
		return "the tree has no declaration"
	case store.CauseInvalidDeclaration:
		return "the declaration is invalid"
	case store.CauseTree:
		return "its tree cannot be computed"
	case store.CauseStore:
		return "the records cannot be read"
	case store.CauseInternal:
		return "of an internal error"
	}
	var parts []string
	if r.Verdict != nil {
		for _, c := range r.Verdict.Missing() {
			parts = append(parts, fmt.Sprintf("`%s` %s", c.ID, stateText(c.State)))
		}
	}
	return strings.Join(parts, " and ")
}

func stateText(state string) string {
	if state == evidence.NotRun {
		return "was not run on this content"
	}
	return state
}

// Observed is the one line an observing hook prints for a ruling that
// would have denied.
func (r Ruling) Observed() string {
	return fmt.Sprintf("cw: the gate only observes: %s would have been denied because %s\n", r.subject(), r.Because())
}

// Denial is the text a hook prints when it denies. It names each check that
// is not satisfied with its state and log, says where the denied content
// differs from the working tree, and ends with the command that produces
// the missing evidence. It names no way around the gate.
func (r Ruling) Denial(st *store.Store) string {
	var b strings.Builder
	if r.Cause != "" {
		fmt.Fprintf(&b, "cw: %s is denied: %s\n", r.subject(), r.Reason)
		if r.Differs != "" {
			b.WriteString(indent(r.Differs) + "\n")
		}
		return b.String()
	}
	fmt.Fprintf(&b, "cw: %s is denied: its content has no recorded passing run of every declared check.\n", r.subject())
	for _, c := range r.Verdict.Missing() {
		fmt.Fprintf(&b, "  %s: %s", c.ID, c.State)
		if c.State == evidence.NotRun {
			b.WriteString(" on this content")
		}
		if c.Deciding != nil {
			if c.Deciding.ExitStatus != nil {
				fmt.Fprintf(&b, " with exit status %d", *c.Deciding.ExitStatus)
			}
			fmt.Fprintf(&b, "; log: %s", st.Abs(c.Deciding.Log))
		}
		b.WriteString("\n")
	}
	if r.Differs != "" {
		b.WriteString(indent(r.Differs) + "\n")
	}
	b.WriteString(r.instruction())
	return b.String()
}

// instruction is the end of a denial: the command that produces the missing
// evidence, and where to run it when the denied content is not what the
// working tree holds.
func (r Ruling) instruction() string {
	switch {
	case !r.Elsewhere:
		return "cw: to produce the missing evidence, run:\n  cw run\n"
	case r.Ref == "":
		return "cw: `cw run` checks the working tree as it is on disk. Make the working tree hold exactly what you commit, by staging the rest or by setting it aside, and run:\n  cw run\n"
	case r.Commit == "":
		return "cw: `cw run` checks the working tree, so it must run in a checkout of that content:\n  cw run\n"
	}
	return fmt.Sprintf("cw: `cw run` checks the working tree, so it must run in a checkout of that commit, for example in a linked worktree:\n  git worktree add --detach ../cw-check %s\nand then, in that directory:\n  cw run\n", r.Commit)
}

func indent(text string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n  ")
}
