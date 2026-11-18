package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

// Only original source identities enter this fingerprint. It makes no claim
// that a host loaded the selected skill or followed its instructions.
func taskVerificationSkills(s *session, task, workflow string, record bool) (string, []verification.SelectionRef, error) {
	config, err := roles.Load(s.repo.Root)
	if err != nil {
		return "", nil, err
	}
	sources := map[string][]compactSkill{}
	refs := []verification.SelectionRef{}
	for _, host := range roles.Hosts {
		for _, role := range append(roles.IDs(), roles.Coordinator) {
			selected := selectedSkills(config, host, role)
			if len(selected) == 0 {
				continue
			}
			catalog, err := skills.Discover(s.repo.Root, host)
			if err != nil {
				return "", nil, err
			}
			entries, err := catalog.Resolve(selected)
			if err != nil {
				return "", nil, err
			}
			for _, entry := range entries {
				sources[host+":"+role] = append(sources[host+":"+role], entry.Reference())
			}
			if record {
				id, err := skills.RecordSelection(s.repo.Root, task, workflow, host, role, entries)
				if err != nil {
					return "", nil, err
				}
				refs = append(refs, verification.SelectionRef{ID: id, Host: host, Role: role})
			}
		}
	}
	return contextpack.Key(sources), refs, nil
}

func taskVerificationLog(s *session, runID string) (string, error) {
	root, err := os.OpenRoot(s.store.Dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat("logs")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("retained log directory unavailable: %v", err)
	}
	return stageRawSource(root, "logs/"+runID+".log")
}

// This private observation is written after the last declared check. Shared
// attempt summaries are historical and cannot manufacture a local observation.
type localVerification struct {
	Version int                   `json:"version"`
	Attempt string                `json:"attempt"`
	Tree    string                `json:"tree"`
	Raw     string                `json:"raw"`
	Policy  string                `json:"policy"`
	Skills  string                `json:"skills"`
	Results []verification.Result `json:"results"`
	Key     string                `json:"key"`
}

func (v localVerification) fingerprint() string {
	v.Key = ""
	return contextpack.Key(v)
}

func localVerificationName(task string) string { return "verification-" + task + ".json" }

func readLocalVerification(s *session, task string) (localVerification, error) {
	var observation localVerification
	root, err := os.OpenRoot(s.repo.GitDir)
	if err != nil {
		return observation, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat("kitsu-stages")
	if err != nil {
		return observation, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return observation, errors.New("unsafe private verification directory")
	}
	name := filepath.Join("kitsu-stages", localVerificationName(task))
	info, err = root.Lstat(name)
	if err != nil {
		return observation, err
	}
	if !info.Mode().IsRegular() || info.Size() > contextpack.MaxArtifact {
		return observation, errors.New("unsafe or oversized private verification observation")
	}
	f, err := root.Open(name)
	if err != nil {
		return observation, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, contextpack.MaxArtifact+1))
	if err != nil || len(data) > contextpack.MaxArtifact {
		return observation, errors.New("private verification observation could not be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueReturnKeys(d, 0); err != nil {
		return observation, err
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&observation); err != nil {
		return observation, err
	}
	if d.Decode(new(any)) != io.EOF || observation.Version != 1 || observation.Results == nil || observation.Key != observation.fingerprint() {
		return observation, errors.New("private verification observation is incomplete or changed")
	}
	return observation, nil
}

func verificationRawKey(ctx context.Context, s *session, tree string) (string, error) {
	raw, err := stageRawSources(ctx, s, tree)
	if err != nil {
		return "", err
	}
	return contextpack.Key(raw), nil
}

// Only the named task's own publication may alter the source baseline between
// prioritized execution and final checks. A concurrent edit is not bookkeeping,
// even when Git's clean filters hide its original bytes.
func verifyPublicationSources(ctx context.Context, s *session, task, before, after string, sources map[string]string) error {
	current, err := stageRawSources(ctx, s, after)
	if err != nil {
		return err
	}
	owned := func(name string) bool {
		return strings.HasPrefix(name, "openspec/changes/"+task+"/verification/") || strings.HasPrefix(name, "openspec/changes/.verification/"+task+"/")
	}
	for name, digest := range sources {
		if !owned(name) && current[name] != digest {
			return errors.New("source changed across verification publication; rerun against current content")
		}
	}
	for name, digest := range current {
		if !owned(name) && sources[name] != digest {
			return errors.New("source changed across verification publication; rerun against current content")
		}
	}
	changes, err := s.repo.Diff(ctx, before, after)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if !owned(change.Path) {
			return errors.New("non-bookkeeping Git source changed across verification publication")
		}
	}
	return nil
}
