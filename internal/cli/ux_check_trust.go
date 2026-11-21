package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/shellwords"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func trustFile(repo *gitx.Repo, suffix string) (uxSnapshot, error) {
	return localTrustFile(repo.CommonDir, suffix)
}

func localTrustFile(gitDir, suffix string) (uxSnapshot, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return uxSnapshot{}, err
	}
	canonical, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return uxSnapshot{}, err
	}
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	return uxReadFile(filepath.Join(dir, "kitsu", "check-trust", name+suffix))
}

func workingCheckPolicy(ctx context.Context, repo *gitx.Repo) (*decl.File, string, error) {
	tree, err := repo.Snapshot(ctx, store.Open(repo.CommonDir).Tmp())
	if err != nil {
		return nil, "", err
	}
	data, fingerprint, err := checkDeclaration(ctx, repo, tree)
	if err != nil || fingerprint == undeclaredPolicy {
		return nil, fingerprint, err
	}
	file, err := decl.Parse(data)
	return file, fingerprint, err
}

func runUXCheckTrust(ctx context.Context, cfg Config, args []string) error {
	if len(args) != 0 && !uxFlagPresent(args, "verify-order") {
		return runCITrust(ctx, cfg, args)
	}
	order, err := parseVerificationOrder(args)
	if err != nil {
		return err
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	if order == reviewFirst {
		return revokeVerificationOrder(ctx, cfg, s.repo)
	}
	file, fingerprint, err := workingCheckPolicy(ctx, s.repo)
	if err != nil {
		return failf("inspect check policy: %v", err)
	}
	if file == nil || len(file.Checks) == 0 {
		return failf("there are no declared commands to approve")
	}
	fmt.Fprintf(cfg.Out, "Checks in %q (SHA-256 %s)\n", s.repo.Root, fingerprint)
	for _, check := range file.Checks {
		fmt.Fprintf(cfg.Out, "  %s %q\n", check.ID, check.Run)
	}
	if !onboardingTerminal(cfg) {
		return usagef("approve the displayed commands in your own terminal")
	}
	return confirmCheckTrust(ctx, cfg, s.repo, fingerprint, order == beforeReview)
}

func confirmCheckTrust(ctx context.Context, cfg Config, repo *gitx.Repo, fingerprint string, early bool) error {
	before, err := trustFile(repo, "")
	if err != nil {
		return err
	}
	var timing uxSnapshot
	if early {
		timing, err = verificationOrderFile(repo)
		if err != nil {
			return err
		}
		fmt.Fprintln(cfg.Out, "These commands may execute candidate code before independent review in this checkout.")
	}
	fmt.Fprintln(cfg.Out, "Type yes to approve the displayed commands and order. Press Ctrl+C to cancel.")
	if err := readCheckApproval(ctx, cfg.In); err != nil {
		return err
	}
	_, current, err := workingCheckPolicy(ctx, repo)
	if err != nil || current != fingerprint {
		return failf("the declaration changed while you were approving it. Inspect the current commands with kitsu check trust")
	}
	if err := uxSaveFile(ctx, before, before.path+".lock", nil, []byte(fingerprint+"\n")); err != nil {
		return err
	}
	fmt.Fprintln(cfg.Out, "Approved the displayed check declaration.")
	if early {
		if err := uxSaveFile(ctx, timing, timing.path+".lock", nil, []byte(fingerprint+"\n")); err != nil {
			return fmt.Errorf("save verify-before-review approval: %w", err)
		}
		fmt.Fprintln(cfg.Out, "Eligible features in this checkout will verify before review.")
	}
	return nil
}

func readCheckApproval(ctx context.Context, input io.Reader) error {
	answer := make(chan bool, 1)
	go func() {
		line, err := bufio.NewReader(io.LimitReader(input, 32)).ReadString('\n')
		answer <- err == nil && strings.TrimSpace(line) == "yes"
	}()
	select {
	case <-ctx.Done():
		return exit(130)
	case yes := <-answer:
		if ctx.Err() != nil {
			return exit(130)
		}
		if !yes {
			return failf("check approval declined")
		}
		return nil
	}
}

var checkExecutionRules = []shellwords.CommandRule{
	{Path: []string{"run"}, Reason: "run"},
	{Path: []string{"check", "run"}, Reason: "run"},
	{Path: []string{"task", "ci"}, Reason: "run"},
	{Path: []string{"task", "test"}, Reason: "run"},
	{Path: []string{"task", "verify"}, Reason: "run"},
	{Path: []string{"task", "commit"}, Reason: "run"}, // May recheck newly staged content.
}

func guardCheckTrust(ctx context.Context, repo *gitx.Repo, command string) string {
	switch shellwords.ChangesCommands(command, []shellwords.CommandRule{
		{Path: []string{"check", "trust"}, Reason: "approve"},
		{Path: []string{"adviser", "story-review"}, Reason: "story-review"},
		{Path: []string{"advisers", "story-review"}, Reason: "story-review"},
	}) {
	case "story-review":
		return "The developer manages automatic story review in their own terminal."
	case "approve":
		return "The developer approves repository checks by running kitsu check trust in their own terminal."
	}
	if shellwords.ChangesCommands(command, checkExecutionRules) == "run" {
		if _, err := os.Stat(filepath.Join(repo.Root, decl.FileName)); os.IsNotExist(err) {
			return ""
		}
		file, fingerprint, err := workingCheckPolicy(ctx, repo)
		if err != nil {
			return "The check declaration could not be inspected. Ask the developer to inspect it with kitsu check list."
		}
		if file == nil || len(file.Checks) == 0 {
			return ""
		}
		approval, err := trustFile(repo, "")
		if err != nil || string(approval.data) != fingerprint+"\n" {
			return "The configured check commands need approval. Ask the developer to inspect and approve them with kitsu check trust in their own terminal."
		}
	}
	return ""
}
