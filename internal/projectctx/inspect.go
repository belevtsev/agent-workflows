package projectctx

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Inspect retains useful marker detection when byte hashing is incomplete. Its
// warnings and partial fingerprint must be preserved by callers.
func Inspect(ctx context.Context, repo string) (Profile, error) {
	got, err := capture(ctx, repo, gitProbeBudget)
	if err != nil {
		return Profile{}, err
	}
	profile := Profile{Snapshot: got.snapshot, Kinds: []string{}, Markers: []Marker{}, Contracts: []Contract{}, Warnings: got.warnings}
	root, err := os.OpenRoot(got.snapshot.Root)
	if err != nil {
		return Profile{}, fmt.Errorf("open project root for inspection")
	}
	defer root.Close()
	for _, path := range got.paths {
		if ctx.Err() != nil {
			return Profile{}, ctx.Err()
		}
		info, statErr := root.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			if statErr != nil && instructionPath(path) {
				profile.Warnings = append(profile.Warnings, "An instruction file could not be safely inspected")
			}
			continue
		}
		kind := ""
		if !ignored(path) {
			kind = markerKind(path)
		}
		if kind != "" {
			if len(profile.Markers) < 2000 {
				profile.Markers = append(profile.Markers, Marker{Kind: kind, Path: filepath.ToSlash(path)})
			} else if !slices.Contains(profile.Warnings, "Project capability markers reached their reporting limit") {
				profile.Warnings = append(profile.Warnings, "Project capability markers reached their reporting limit")
			}
			if !slices.Contains(profile.Kinds, kind) {
				profile.Kinds = append(profile.Kinds, kind)
			}
		}
		if instructionPath(path) {
			file, openErr := root.Open(path)
			if openErr != nil {
				profile.Warnings = append(profile.Warnings, "An instruction file could not be safely read")
				continue
			}
			hash := sha256.New()
			read, readErr := io.Copy(hash, io.LimitReader(file, 1<<20+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil || read > 1<<20 {
				profile.Warnings = append(profile.Warnings, "An instruction file exceeded its read limit or was unreadable")
				continue
			}
			profile.Contracts = append(profile.Contracts, Contract{Path: filepath.ToSlash(path), SHA256: fmt.Sprintf("%x", hash.Sum(nil))})
		}
	}
	slices.Sort(profile.Kinds)
	return profile, nil
}

func markerKind(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "go.mod", "go.work":
		return "go"
	case "package.json":
		return "node"
	case "pyproject.toml", "setup.py", "setup.cfg", "pipfile", "requirements.txt":
		return "python"
	case "default.project.json", "rojo.json", "aftman.toml", "rokit.toml", ".luaurc", "selene.toml":
		return "roblox"
	}
	if strings.HasSuffix(base, ".tf") || strings.HasSuffix(base, ".tf.json") {
		return "terraform"
	}
	if strings.HasSuffix(base, ".project.json") || strings.HasSuffix(base, ".rbxlx") || strings.HasSuffix(base, ".rbxmx") {
		return "roblox"
	}
	if strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt") {
		return "python"
	}
	return ""
}

func instructionPath(path string) bool {
	path = filepath.ToSlash(path)
	switch strings.ToLower(filepath.Base(path)) {
	case "agents.md", "claude.md", "gemini.md", ".cursorrules", "copilot-instructions.md":
		return true
	}
	return strings.Contains(path, ".cursor/rules/") && strings.HasSuffix(path, ".mdc")
}
