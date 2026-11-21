package gate

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

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
	// Elsewhere says that it does, so `kitsu run` here checks other content.
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
		ruling.Cause, ruling.Reason = store.CauseNoDeclaration, "the tree has no declaration: it holds no "+decl.FileName+" at its root. When that is on purpose, see "+NoDeclarationURL
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
			changed = append(changed, fmt.Sprintf("`run` was %s and becomes %s", listText(old.Run), listText(current.Run)))
		}
		if !slices.Equal(old.Paths, current.Paths) {
			changed = append(changed, fmt.Sprintf("`paths` was %s and becomes %s", listText(old.Paths), listText(current.Paths)))
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

// listText is a list as kitsu.toml holds it.
func listText(items []string) string {
	if len(items) == 0 {
		return "not set"
	}
	return decl.QuoteList(items)
}

// compareWorktree says whether `kitsu run` in the working tree would check the
// denied content. It would when the working tree holds that content, or the
// same content for every check that is not satisfied. Otherwise it says how
// the two differ. The snapshot is taken only for a denial.
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
	work := g.rule(ctx, ruling.Hook, snapshot)
	if sameMissing(ruling.Verdict, work.Verdict) {
		return
	}
	ruling.Elsewhere = true
	ruling.Differs = "The working tree holds other content than this commit."
	paths, more, err := g.Repo.DiffPaths(ctx, ruling.Tree, snapshot)
	if err != nil {
		return
	}
	ruling.Differs += " Paths that differ:\n"
	for _, path := range paths {
		ruling.Differs += "  " + decl.Plain(path) + "\n"
	}
	if more > 0 {
		ruling.Differs += fmt.Sprintf("  ... and %d more\n", more)
	}
	if work.Allowed {
		ruling.Differs += "The working tree as a whole is verified; this commit is not, because it records other content."
	} else {
		ruling.Differs += "The working tree as a whole is not verified either."
	}
}

// sameMissing says whether every check the denied tree misses has the same
// command and the same checked content in the working tree, so that a run
// there is a run on the denied content.
func sameMissing(denied, work *evidence.Verdict) bool {
	if denied == nil || work == nil {
		return false
	}
	for _, c := range denied.Missing() {
		if !slices.ContainsFunc(work.Checks, func(w evidence.Check) bool {
			return w.ID == c.ID && w.Key == c.Key && slices.Equal(w.Run, c.Run) && slices.Equal(w.Paths, c.Paths)
		}) {
			return false
		}
	}
	return true
}

// zeroID says whether an object id is the all-zero id git uses for "none".
func zeroID(id string) bool { return strings.Trim(id, "0") == "" }

// Push judges each ref a push updates. git gives one line per ref on input:
// the local ref and its id, then the remote ref and its id.
func (g *Gate) Push(ctx context.Context, remote string, input io.Reader) []Ruling {
	const hook = "pre-push"
	var updates [][]string
	var tips []string // the new tips of the branches this push updates
	reader := bufio.NewReaderSize(input, 64<<10)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, io.EOF) && len(line) == 0 {
			break
		}
		fields := strings.Fields(string(line))
		if err != nil || len(fields) != 4 || !validPushID(fields[1]) ||
			len(fields[1]) != len(fields[3]) || !validPushID(fields[3]) ||
			!strings.HasPrefix(fields[2], "refs/") {
			return []Ruling{Cannot(hook, store.CauseInternal, "Git did not supply a complete, valid ref update")}
		}
		updates = append(updates, fields)
		if strings.HasPrefix(fields[2], "refs/heads/") && !zeroID(fields[1]) {
			tips = append(tips, fields[1])
		}
	}
	if len(updates) == 0 {
		return []Ruling{{Hook: hook, Allowed: true, Cause: store.CauseNoRefUpdates}}
	}

	var rulings []Ruling
	for _, update := range updates {
		ruling := g.pushRef(ctx, remote, update[1], update[2], update[3], tips)
		ruling.Hook, ruling.Ref = hook, update[2]
		rulings = append(rulings, ruling)
	}
	return rulings
}

func validPushID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// pushRef judges one update. A deletion and an update that adds nothing to
// the remote are allowed; otherwise the tree of the new tip must be
// verified. Only the tip is judged, not the commits below it.
func (g *Gate) pushRef(ctx context.Context, remote, localID, remoteRef, remoteID string, tips []string) Ruling {
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
		adds, err := g.addsSomething(ctx, remote, commit, remoteRef, remoteID, tips)
		if err != nil {
			return Cannot(hook, store.CauseInternal, err.Error())
		}
		if !adds {
			return Ruling{Allowed: true, Commit: commit}
		}
	}
	tree, found, err := g.Repo.TreeOf(ctx, localID)
	if err != nil {
		return Cannot(hook, store.CauseInternal, err.Error())
	}
	if !found {
		return Cannot(hook, store.CauseTree, "it names neither a commit nor a tree, so there is nothing to judge")
	}
	ruling := g.rule(ctx, hook, tree)
	ruling.Commit = commit
	if !ruling.Allowed && ruling.Verdict != nil {
		g.compareWorktree(ctx, &ruling)
		ruling.Differs = "" // the denial names the ref; the paths of another branch say nothing more
	}
	return ruling
}

// addsSomething says whether pushing the commit to the ref gives the remote
// a commit it does not have. For a branch, the commit the branch points at
// on the remote decides. For any other ref, the branches this push updates
// and the remote-tracking branches decide; notes hold no content of the
// project and are not judged.
func (g *Gate) addsSomething(ctx context.Context, remote, commit, remoteRef, remoteID string, tips []string) (bool, error) {
	if strings.HasPrefix(remoteRef, "refs/heads/") {
		if zeroID(remoteID) {
			return true, nil
		}
		contained, err := g.Repo.Contains(ctx, commit, remoteID)
		return !contained, err
	}
	if strings.HasPrefix(remoteRef, "refs/notes/") {
		return false, nil
	}
	for _, tip := range tips {
		if contained, err := g.Repo.Contains(ctx, commit, tip); contained || err != nil {
			return false, err
		}
	}
	held, err := g.Repo.RemoteHolds(ctx, remote, commit)
	return !held, err
}

// Decision is the record of a ruling.
func (r Ruling) Decision(mode string, now time.Time) store.Decision {
	d := store.Decision{Time: now.UTC().Truncate(time.Second), Hook: r.Hook, Mode: mode, Tree: r.Tree, Ref: r.Ref, Outcome: "allowed", Cause: r.Cause}
	if r.Allowed {
		return d // with its cause: a push that updated nothing is told from one that was judged
	}
	d.Outcome = "denied"
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
	return fmt.Sprintf("kitsu: the gate only observes: %s would have been denied because %s\n", r.subject(), r.Because())
}

// Denial is the text a hook prints when it denies. It names each check that
// is not satisfied with its state and log, says where the denied content
// differs from the working tree, and ends with the command that produces
// the missing evidence. It names no way around the gate.
func (r Ruling) Denial(st *store.Store) string {
	var b strings.Builder
	if r.Cause != "" {
		fmt.Fprintf(&b, "kitsu: %s is denied: %s\n", r.subject(), r.Reason)
		if r.Differs != "" {
			b.WriteString(indent(r.Differs) + "\n")
		}
		return b.String()
	}
	fmt.Fprintf(&b, "kitsu: %s is denied: its content has no recorded passing run of every declared check.\n", r.subject())
	for _, c := range r.Verdict.Missing() {
		fmt.Fprintf(&b, "  %s: %s\n", c.ID, Describe(st, c))
	}
	if r.Differs != "" {
		b.WriteString(indent(r.Differs) + "\n")
	}
	b.WriteString(r.instruction())
	return b.String()
}

// Describe is the state of a check as a person reads it. The state comes
// first: satisfied, not run, failed or timed out. A run that decides is
// named with its log, and a last run that decides nothing with how it ended.
func Describe(st *store.Store, c evidence.Check) string {
	text := c.State
	switch {
	case c.State == evidence.NotRun:
		text += " on this content"
		if c.Last != nil {
			text += fmt.Sprintf(" (the last run ended %s", c.Last.Outcome)
			if c.Last.Detail != "" {
				text += ": " + c.Last.Detail
			}
			text += ")"
		}
	case c.Implied:
		text += " (same checked content as the base revision)"
	case c.Deciding != nil && c.State != evidence.Satisfied:
		if c.Deciding.ExitStatus != nil {
			text += fmt.Sprintf(" with exit status %d", *c.Deciding.ExitStatus)
		}
		if c.Deciding.Detail != "" {
			text += ": " + c.Deciding.Detail
		}
		text += "; log: " + st.Abs(c.Deciding.Log)
	}
	if c.Running {
		text += " (a run is executing now)"
	}
	return text
}

// instruction is the end of a denial: what to fix first when a check failed,
// the command that produces the missing evidence, and where to run it when
// the denied content is not what the working tree holds.
func (r Ruling) instruction() string {
	fix := ""
	if slices.ContainsFunc(r.Verdict.Missing(), func(c evidence.Check) bool { return c.Deciding != nil }) {
		fix = "kitsu: a check failed or timed out; first fix what its log shows.\n"
	}
	switch {
	case !r.Elsewhere:
		return fix + "kitsu: to produce the missing evidence, run:\n  kitsu run\n"
	case r.Ref == "":
		return fix + "kitsu: `kitsu run` checks the working tree as it is on disk. Make the working tree hold exactly what you commit, by staging the rest (git add -A) or by setting it aside (git stash push --keep-index --include-untracked), and run:\n  kitsu run\n"
	case r.Commit == "":
		return fix + "kitsu: `kitsu run` checks the working tree, so it must run in a checkout of that content:\n  kitsu run\n"
	}
	return fix + fmt.Sprintf("kitsu: `kitsu run` checks the working tree, so it must run in a checkout of that commit, for example in a linked worktree:\n  git worktree add --detach ../kitsu-check %s\nand then, in that directory:\n  kitsu run\n", r.Commit)
}

func indent(text string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n  ")
}
