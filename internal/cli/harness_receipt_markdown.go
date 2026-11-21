package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/tickets"
)

type savedVerification struct {
	candidate harness.Snapshot
	completed *harness.Completion
	proof     *harness.Proof
	order     string
	commit    string
}

var receiptObjectID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// Markdown export reads only the private ledger and objects and logs already
// named by the record. It does not observe the live checkout or run a check.
func printMarkdownReceipt(ctx context.Context, s *session, id string) error {
	ledger, err := readOnlyStageLedger(s.repo.GitDir)
	if err != nil {
		return failf("no readable saved task %s", id)
	}
	defer ledger.close()
	var task harness.Task
	if err := ledger.read(harnessFile(id), &task); err != nil || !task.Valid(id) {
		return failf("no readable saved task %s", id)
	}
	result := receiptVerification(ctx, s, &task)
	if !receiptObjectID.MatchString(result.candidate.Tree) {
		return failf("no completed verification evidence for task %s", id)
	}
	md := &strings.Builder{}
	fmt.Fprintf(md, "# Kitsu task receipt\n\n- Task `%s`\n- Verified tree `%s`\n", id, result.candidate.Tree)
	result.writeDelivery(md, &task)
	if task.Bug != nil {
		writeReceiptTicket(md, task.Bug.Source)
	}
	declaration, declarationErr := evidence.Declaration(ctx, s.repo, result.candidate.Tree)
	fmt.Fprintln(md, "- Repository declaration ·", receiptDeclaration(&task, result.proof, declarationErr))
	fmt.Fprintln(md, "- Review ·", receiptReview(ctx, s, &task, result))
	writeReceiptChecks(md, s, &task, result, declaration, declarationErr)
	_, err = fmt.Fprint(s.cfg.Out, md.String())
	return err
}

func receiptVerification(ctx context.Context, s *session, task *harness.Task) savedVerification {
	var result savedVerification
	if p := task.Proof; p != nil && p.Outcome == store.Passed && p.PerRunInputs && p.IndexChecked && p.GateFailure == nil && p.FailedCheck == "" &&
		!p.CheckedAt.IsZero() && !task.NeedsReproduction() {
		result.candidate, result.proof = p.Candidate, p
	}
	if c := task.CompletedDelivery(); c != nil {
		result.completed, result.candidate = c, c.Candidate
		result.order, result.commit = c.CommitEvidence, c.Candidate.Head
		if result.proof != nil && result.proof.Candidate.Tree != result.candidate.Tree {
			result.proof = nil
		}
	} else if result.proof != nil {
		result.commit, result.order = receiptVerifiedCommit(ctx, s, task, result.candidate)
	}
	if !receiptObjectID.MatchString(result.commit) {
		result.commit = ""
	}
	return result
}

func receiptVerifiedCommit(ctx context.Context, s *session, task *harness.Task, c harness.Snapshot) (string, string) {
	if c.Tree != c.Index {
		return "", ""
	}
	if receiptObjectID.MatchString(c.Head) && c.MergeHeads == "" {
		if tree, _, err := s.repo.CommitContents(ctx, c.Head); err == nil && tree == c.Tree {
			return c.Head, "verified_existing_commit"
		}
	}
	if a, recorded := task.AttemptedCommit(); recorded && receiptObjectID.MatchString(a.Commit) &&
		a.Tree == c.Tree && a.Before.SameBase(c) {
		tree, parents, err := s.repo.CommitContents(ctx, a.Commit)
		if err == nil && tree == c.Tree && parents == c.Parents() {
			return a.Commit, "commit_matches_verified_tree"
		}
	}
	return "", ""
}

func (r savedVerification) writeDelivery(md *strings.Builder, task *harness.Task) {
	switch {
	case r.commit != "":
		fmt.Fprintf(md, "- Verified commit `%s`\n", r.commit)
	case r.completed != nil:
		fmt.Fprintln(md, "- Committed tree. The saved receipt has no exact commit ID.")
	default:
		fmt.Fprintln(md, "- Verified patch (saved snapshot).")
	}
	switch r.order {
	case "verified_existing_commit":
		fmt.Fprintln(md, "- The commit was already checked out when verification ran.")
	case "commit_matches_verified_tree":
		fmt.Fprintln(md, "- Verification preceded the observed commit with the matching tree and parents.")
	default:
		if r.completed != nil {
			fmt.Fprintln(md, "- The saved evidence gives no commit sequence.")
		}
	}
	if r.proof != nil {
		fmt.Fprintln(md, "- Verified at", r.proof.CheckedAt.UTC().Format(time.RFC3339))
	}
	if r.completed != nil && !r.completed.At.IsZero() {
		fmt.Fprintln(md, "- Delivery recorded at", r.completed.At.UTC().Format(time.RFC3339))
	}
	if task.Scope.Publish != "" {
		if r.completed != nil {
			fmt.Fprintln(md, "- Publication was confirmed at delivery.")
		} else {
			fmt.Fprintln(md, "- Publication is pending.")
		}
	}
}

func writeReceiptTicket(md *strings.Builder, source harness.TicketSource) {
	link := tickets.CanonicalIssueLink(source.URL)
	pr := tickets.CanonicalPullRequestLink(source.URL)
	switch {
	case source.Check != nil && pr != "":
		fmt.Fprintf(md, "- Pull request <%s>\n", pr)
		fmt.Fprintln(md, "- Reported failed check", markdownCode(source.Check.Name), "on commit", markdownCode(source.Check.Head))
	case link != "":
		fmt.Fprintf(md, "- Ticket <%s>\n", link)
	case source.Kind == "markdown":
		fmt.Fprintln(md, "- Ticket came from a local Markdown file.")
	case source.Kind == "supplied":
		fmt.Fprintln(md, "- Ticket supplied by the host. The source reference stays in the local record.")
	case source.Kind == "url":
		fmt.Fprintln(md, "- Ticket imported from a URL. The source reference stays in the local record.")
	default:
		fmt.Fprintln(md, "- Ticket source reference withheld.")
	}
	if stageValidID(source.SHA256) {
		fmt.Fprintf(md, "- Ticket content SHA-256 `%s`\n", source.SHA256)
	}
}

func markdownCode(value string) string {
	value = decl.Plain(value)
	fence := "`"
	for strings.Contains(value, fence) {
		fence += "`"
	}
	return fence + " " + value + " " + fence
}

func receiptDeclaration(task *harness.Task, proof *harness.Proof, declarationErr error) string {
	if proof != nil && proof.RepositoryPolicy == task.RepositoryPolicy {
		switch pin := proof.RepositoryPolicy; {
		case pin == undeclaredPolicy:
			return "Undeclared on the verified tree, as pinned at intake."
		case stageValidID(pin):
			return "Pinned SHA-256 `" + pin + "`."
		}
	}
	if errors.Is(declarationErr, evidence.ErrNoDeclaration) {
		return "Undeclared on the verified tree."
	}
	return "No approved declaration pin recorded."
}

func receiptReview(ctx context.Context, s *session, task *harness.Task, result savedVerification) string {
	review := task.Latest("review")
	if review == nil {
		required, err := repositoryRequiresReview(ctx, s, task, result.candidate)
		if task.Scope.RequireReview || task.Scope.Entry == "review" || required {
			return "Required review pending."
		}
		if err != nil {
			return "No review recorded. Repository review policy unavailable."
		}
		return "No review recorded."
	}
	c := result.candidate
	var tree, parents string
	if receiptObjectID.MatchString(c.Head) {
		tree, parents, _ = s.repo.CommitContents(ctx, c.Head)
	}
	if !review.ReviewMatches(c, task.Scope.Until, tree, parents) {
		return "Only an earlier candidate has a recorded review."
	}
	level := "Agent-reported review"
	if native := review.NativeReview; native != nil && native.Parent != "" && native.Worker != "" {
		switch native.Host {
		case "codex":
			level = "Host-observed review (Codex)"
		case "claude":
			level = "Host-observed review (Claude Code)"
		}
	}
	if n := len(review.Findings); n > 0 {
		return fmt.Sprintf("%s recorded %d finding(s).", level, n)
	}
	return level + " has no open findings."
}

func writeReceiptChecks(md *strings.Builder, s *session, task *harness.Task, result savedVerification, declaration *decl.File, declarationErr error) {
	md.WriteString("\n## Verification evidence\n\n")
	var bugReceipt *harness.DeliveryReceipt
	if bug := task.Bug; bug != nil {
		if red := bug.Reproduction; red != nil && red.Outcome == store.Failed {
			run := harness.CheckRun{ID: red.Run, Digest: red.LogDigest}
			fmt.Fprintln(md, "- Bug reproduction ·", receiptCheckRun(s, run, "Failed as expected"))
		}
		if saved := bug.Receipt; saved != nil && saved.Commit == result.commit && saved.Tree == result.candidate.Tree {
			bugReceipt = saved
			if receiptObjectID.MatchString(saved.BeforeCommitTree) {
				fmt.Fprintf(md, "- Behavior before the hook on tree `%s` · %s\n", saved.BeforeCommitTree, receiptCheckRun(s, saved.BeforeCommit, "Passed"))
			}
		}
	}
	p := result.proof
	var runs map[string]harness.CheckRun
	switch {
	case p != nil:
		runs = p.GateRuns
		run := harness.CheckRun{ID: p.Run, Digest: p.LogDigest}
		fmt.Fprintln(md, "- Behavior assertion ·", receiptCheckRun(s, run, "Passed"))
	case bugReceipt != nil && bugReceipt.BeforeCommitTree == "":
		fmt.Fprintln(md, "- Behavior before commit ·", receiptCheckRun(s, bugReceipt.BeforeCommit, "Passed"))
	default:
		fmt.Fprintln(md, "- Behavior assertion · Saved run details unavailable.")
	}
	if runs == nil && bugReceipt != nil {
		runs = bugReceipt.RepositoryRuns
	}
	writeReceiptRepositoryChecks(md, s, runs, declaration, declarationErr)
	if bugReceipt != nil {
		fmt.Fprintln(md, "\n- Behavior on the delivered commit ·", receiptCheckRun(s, bugReceipt.OnCommit, "Passed"))
	} else if task.Bug != nil {
		fmt.Fprintln(md, "\n- Behavior on the delivered commit · Ticket delivery receipt pending.")
	}
}

func writeReceiptRepositoryChecks(md *strings.Builder, s *session, runs map[string]harness.CheckRun, declaration *decl.File, declarationErr error) {
	switch {
	case errors.Is(declarationErr, evidence.ErrNoDeclaration):
		fmt.Fprintln(md, "- Repository checks · No declaration on the verified tree.")
	case declarationErr != nil:
		fmt.Fprintln(md, "- Repository checks · Declaration unavailable.")
	case len(declaration.Checks) == 0:
		fmt.Fprintln(md, "- Repository checks · None declared.")
	default:
		fmt.Fprintln(md, "\n| Repository check | Saved verdict |\n| --- | --- |")
		for _, check := range declaration.Checks {
			verdict := "No saved result"
			if run, ok := runs[check.ID]; ok {
				verdict = receiptCheckRun(s, run, "Passed")
			}
			fmt.Fprintf(md, "| `%s` | %s |\n", check.ID, verdict)
		}
	}
}

func receiptCheckRun(s *session, run harness.CheckRun, verdict string) string {
	if !filepath.IsLocal(run.ID) || filepath.Base(run.ID) != run.ID || !stageValidID(run.Digest) {
		return "Saved run details unavailable"
	}
	digest, err := taskVerificationLog(s, run.ID)
	if err != nil || digest != run.Digest {
		return verdict + " (recorded, matching local log unavailable)"
	}
	return verdict
}
