package cli

import (
	json "encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for session start when Codex resumes a thread. Codex runs the
// SessionStart hook on startup, resume and compact; a resumed thread keeps
// what earlier session starts added, and Codex adds the context again after
// a compaction (docs/hooks.md, Codex). testdata/codex-session-start.json is
// the SessionStart input codex-cli 0.167.2 sent on compact, with its paths
// replaced by placeholders; it sent the same members on startup and resume.

// hkCodexSessionStart runs session-start as hooks/codex-hooks.json does,
// with --host codex, the recorded input for repo and source, the
// instructions file and the cache, and returns what the hook printed.
func hkCodexSessionStart(t *testing.T, h *hkHarness, repo, source, instructions, cache string) string {
	t.Helper()
	stdin := hkRecordedInput(t, "codex-session-start.json", repo, map[string]any{"source": source})
	got, err := h.runSessionStart(t, string(stdin), "--host", "codex", "--instructions", instructions, "--cache", cache)
	if err != nil {
		t.Fatalf("session-start on %s: %v", source, err)
	}
	return got
}

// hkExpectNoSessionContext requires one SessionStart JSON line with no
// additionalContext: the hook command prints it as it is, and the host adds
// nothing to the session.
func hkExpectNoSessionContext(t *testing.T, got string) {
	t.Helper()
	if !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatalf("hook wrote %q, want exactly one newline-terminated line", got)
	}
	var whole map[string]map[string]any
	if err := json.Unmarshal([]byte(got), &whole); err != nil {
		t.Fatalf("session-start line %q is not a JSON object of objects: %v", got, err)
	}
	output, ok := whole["hookSpecificOutput"]
	if len(whole) != 1 || !ok || len(output) != 1 || output["hookEventName"] != "SessionStart" {
		t.Fatalf("session-start object %v, want only hookSpecificOutput with hookEventName SessionStart", whole)
	}
}

// hkNoSuggestionRecorded requires that cache holds no cw gate install
// suggestion.
func hkNoSuggestionRecorded(t *testing.T, cache string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cache, gateSuggestedDirectory))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache records a cw gate install suggestion for %v, want none", entries)
	}
}

func TestHookSessionStartAddsNothingWhenCodexResumes(t *testing.T) {
	// Oracle: when Codex resumes a thread, the hook prints one SessionStart
	// object without additionalContext and records no cw gate install
	// suggestion, with the plugin options at their defaults and with both
	// gate and doctor options off, when cw reads stdin only for the source.
	// Printing nothing would make the hook command print the file instead.
	// The controls keep their context: Codex on startup and compact, Claude
	// Code (no --host) on resume, also where PLUGIN_ROOT is set, which no
	// longer names the host, and Codex input that cannot be read, which gives
	// no source. Each case has a cache of its own, in a
	// repository with an open substantial task and no git hooks.
	const instructions = "# Rules\n\nWork carefully."
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	file := hkWriteInstructions(t, instructions)
	withLine := instructions + "\n\n" + hkGateLine
	repo := hkRepo(t)
	h := newHKHarness(t)
	h.start(t, repo, hkSubstantialPlan)

	t.Run("Codex resume", func(t *testing.T) {
		cache := hkCanonicalTempDir(t)
		hkExpectNoSessionContext(t, hkCodexSessionStart(t, h, repo, "resume", file, cache))
		hkNoSuggestionRecorded(t, cache)
	})
	t.Run("Codex resume with the gate and doctor options off", func(t *testing.T) {
		h.env[enforceVerifyOption] = "false"
		h.env[sessionDoctorOption] = "false"
		defer delete(h.env, enforceVerifyOption)
		defer delete(h.env, sessionDoctorOption)
		hkExpectNoSessionContext(t, hkCodexSessionStart(t, h, repo, "resume", file, hkCanonicalTempDir(t)))
	})
	for _, source := range []string{"startup", "compact"} {
		t.Run("Codex "+source, func(t *testing.T) {
			if got := hkSessionContext(t, hkCodexSessionStart(t, h, repo, source, file, hkCanonicalTempDir(t))); got != withLine {
				t.Fatalf("additionalContext %q, want %q", got, withLine)
			}
		})
	}
	for name, pluginRoot := range map[string]string{"Claude Code resume": "", "Claude Code resume with PLUGIN_ROOT set": filepath.Dir(file)} {
		t.Run(name, func(t *testing.T) {
			if pluginRoot != "" {
				h.env["PLUGIN_ROOT"] = pluginRoot
				defer delete(h.env, "PLUGIN_ROOT")
			}
			// The members Claude Code 2.1.311 sent on startup, with source resume.
			stdin, err := json.Marshal(map[string]any{"session_id": "s", "transcript_path": "t", "cwd": repo, "hook_event_name": "SessionStart", "source": "resume"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := h.runSessionStart(t, string(stdin), "--instructions", file, "--cache", hkCanonicalTempDir(t))
			if err != nil {
				t.Fatalf("session-start: %v", err)
			}
			if context := hkSessionContext(t, got); context != withLine {
				t.Fatalf("additionalContext %q, want %q", context, withLine)
			}
		})
	}
	t.Run("Codex input that cannot be read", func(t *testing.T) {
		got, err := h.runSessionStart(t, `{"source":"resume"`, "--host", "codex", "--instructions", file, "--cache", hkCanonicalTempDir(t))
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		if context := hkSessionContext(t, got); context != withLine {
			t.Fatalf("additionalContext %q, want %q", context, withLine)
		}
	})
}

func TestHookSessionStartSuggestsGateInstallOnceWhenCodexResumesAndCompacts(t *testing.T) {
	// Oracle: Codex ran the resume and compact SessionStart hooks of one
	// turn 28 ms apart (docs/hooks.md, Codex). The resume run neither
	// prints the cw gate install line nor records it, so with one cache the
	// pair prints the line once, from compact, in either order, and a later
	// compact prints the instructions alone. As the resume run never reads
	// or writes the suggestion, running the two at the same moment cannot
	// print it twice either.
	const instructions = "# Rules\n\nWork carefully."
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	file := hkWriteInstructions(t, instructions)
	withLine := instructions + "\n\n" + hkGateLine
	for _, order := range [][]string{{"resume", "compact", "compact"}, {"compact", "resume", "compact"}} {
		t.Run(strings.Join(order, " then "), func(t *testing.T) {
			repo := hkRepo(t)
			h := newHKHarness(t)
			h.start(t, repo, hkSubstantialPlan)
			cache := hkCanonicalTempDir(t)
			lines := 0
			for i, source := range order {
				got := hkCodexSessionStart(t, h, repo, source, file, cache)
				if source == "resume" {
					hkExpectNoSessionContext(t, got)
					continue
				}
				context := hkSessionContext(t, got)
				if context != instructions && context != withLine {
					t.Fatalf("run %d (%s): additionalContext %q, want the instructions with or without the line", i+1, source, context)
				}
				if context == withLine {
					lines++
				}
			}
			if lines != 1 {
				t.Fatalf("the cw gate install line was printed %d times, want once", lines)
			}
		})
	}
}
