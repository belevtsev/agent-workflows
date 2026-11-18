package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type githubCLI func(...string) ([]byte, error)

type gitRef struct {
	Ref    string `json:"ref"`
	Object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"object"`
}

type release struct {
	ID         int64          `json:"id"`
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Immutable  bool           `json:"immutable"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Digest string `json:"digest"`
}

func githubJSON[T any](gh githubCLI, args ...string) (T, error) {
	var result T
	data, err := gh(args...)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("decode GitHub response: %w", err)
	}
	return result, nil
}

func ensureTag(gh githubCLI, repo, tag, commit string) error {
	ref := "refs/tags/" + tag
	matches, err := githubJSON[[]gitRef](gh, "api", repo+"/git/matching-refs/tags/"+tag)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(matches, func(candidate gitRef) bool { return candidate.Ref == ref })
	var selected gitRef
	if i < 0 {
		selected, err = githubJSON[gitRef](gh, "api", "--method", "POST", repo+"/git/refs", "-f", "ref="+ref, "-f", "sha="+commit)
		if err != nil {
			return err
		}
	} else {
		selected = matches[i]
	}
	if selected.Ref != ref || selected.Object.Type != "commit" || selected.Object.SHA != commit {
		return fmt.Errorf("tag %s does not point to %s", tag, commit)
	}
	return nil
}

func findRelease(gh githubCLI, repo, tag string) (release, error) {
	// Authenticated listings also include unfinished drafts.
	pages, err := githubJSON[[][]release](gh, "api", "--paginate", "--slurp", repo+"/releases?per_page=100")
	if err != nil {
		return release{}, err
	}
	var found release
	for _, page := range pages {
		for _, entry := range page {
			if entry.Tag == tag {
				if found.ID != 0 {
					return release{}, fmt.Errorf("multiple releases exist for %s", tag)
				}
				found = entry
			}
		}
	}
	return found, nil
}

func checkDraft(draft release, tag string, prerelease bool, expected map[string]string) ([]string, error) {
	if draft.ID == 0 || !draft.Draft || draft.Tag != tag || draft.Prerelease != prerelease {
		return nil, fmt.Errorf("release %s is not a compatible draft", tag)
	}
	seen := make(map[string]bool, len(draft.Assets))
	for _, asset := range draft.Assets {
		digest, ok := expected[asset.Name]
		if !ok || seen[asset.Name] || asset.State != "uploaded" || asset.Digest != digest {
			return nil, fmt.Errorf("draft %s has an unexpected asset or digest for %s", tag, asset.Name)
		}
		seen[asset.Name] = true
	}
	missing := make([]string, 0, len(expected)-len(seen))
	for name := range expected {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing, nil
}

func publishRelease(gh githubCLI, repository, tag, commit string, expected map[string]string) error {
	repo := "repos/" + repository
	settings, err := githubJSON[struct {
		Enabled bool `json:"enabled"`
	}](gh, "api", repo+"/immutable-releases")
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return errors.New("enable immutable releases before publishing")
	}
	draft, err := findRelease(gh, repo, tag)
	if err != nil {
		return err
	}
	prerelease := strings.Contains(tag, "-")
	if draft.ID != 0 {
		if _, err := checkDraft(draft, tag, prerelease, expected); err != nil {
			return err
		}
	}
	// GitHub ignores target_commitish when a tag exists. The ref is authoritative.
	if err := ensureTag(gh, repo, tag, commit); err != nil {
		return err
	}
	if draft.ID == 0 {
		draft, err = githubJSON[release](gh, "api", "--method", "POST", repo+"/releases", "-f", "tag_name="+tag,
			"-f", "target_commitish="+commit, "-f", "name="+tag, "-F", "body=@"+notesName, "-F", "draft=true",
			"-F", "prerelease="+strconv.FormatBool(prerelease))
		if err != nil {
			return err
		}
	}
	missing, err := checkDraft(draft, tag, prerelease, expected)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		if _, err := gh(append([]string{"release", "upload", tag}, missing...)...); err != nil {
			return err
		}
	}
	endpoint := repo + "/releases/" + strconv.FormatInt(draft.ID, 10)
	uploaded, err := githubJSON[release](gh, "api", endpoint)
	if err != nil {
		return err
	}
	missing, err = checkDraft(uploaded, tag, prerelease, expected)
	if err != nil {
		return err
	}
	if len(missing) != 0 {
		return fmt.Errorf("draft %s still needs assets: %s", tag, strings.Join(missing, ", "))
	}
	published, err := githubJSON[release](gh, "api", "--method", "PATCH", endpoint, "-F", "draft=false",
		"-f", "make_latest="+strconv.FormatBool(!prerelease))
	if err != nil {
		return err
	}
	if published.ID != draft.ID || published.Tag != tag || published.Draft || !published.Immutable {
		return fmt.Errorf("GitHub did not confirm that %s is an immutable release", tag)
	}
	return nil
}
