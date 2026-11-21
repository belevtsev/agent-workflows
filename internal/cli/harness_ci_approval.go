package cli

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

func ciTrustFile(repo *gitx.Repo, id string) (uxSnapshot, error) {
	return trustFile(repo, fmt.Sprintf(".ci-%x.toml", sha256.Sum256([]byte(id))))
}

func runCITrust(ctx context.Context, cfg Config, args []string) error {
	f := newFlags("check trust")
	id := f.String("ci", "", "")
	p, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	if *id == "" || len(p.pos) != 0 || !p.dash || len(p.rest) == 0 {
		return usagef("use kitsu check trust --ci ID -- PROGRAM ARGS")
	}
	data, err := decl.Append([]byte("version = 1\n"), *id, p.rest, nil, "")
	if err != nil {
		return usagef("%v", err)
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.Out, "Local CI check %q in %q\n  %q\n", *id, s.repo.Root, p.rest)
	if !onboardingTerminal(cfg) {
		return usagef("approve the displayed command in your own terminal")
	}
	before, err := ciTrustFile(s.repo, *id)
	if err != nil {
		return err
	}
	fmt.Fprintln(cfg.Out, "Type yes to let Kitsu use this command for CI repair. Press Ctrl+C to cancel.")
	if err := readCheckApproval(ctx, cfg.In); err != nil {
		return err
	}
	if err := uxSaveFile(ctx, before, before.path+".lock", nil, data); err != nil {
		return err
	}
	fmt.Fprintf(cfg.Out, "Approved local CI check %q. Select it with --check %s.\n", *id, *id)
	return nil
}

func readApprovedLocalCI(repo *gitx.Repo, id string) (decl.Check, string, error) {
	saved, err := ciTrustFile(repo, id)
	if err != nil {
		return decl.Check{}, "", err
	}
	if !saved.exists {
		return decl.Check{}, "", failf("no declared or approved local check %q. Use kitsu check list or approve a reproduction in your terminal with kitsu check trust --ci ID -- PROGRAM ARGS", id)
	}
	policy, err := decl.Parse(saved.data)
	if err != nil || len(policy.Checks) != 1 || policy.Checks[0].ID != id {
		return decl.Check{}, "", failf("invalid approval for local check %q. Approve it again in your terminal with kitsu check trust --ci ID -- PROGRAM ARGS", id)
	}
	return policy.Checks[0], fmt.Sprintf("%x", sha256.Sum256(saved.data)), nil
}
