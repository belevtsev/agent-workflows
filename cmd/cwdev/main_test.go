package main

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDeveloperArgumentsRejectIncompleteOrUnknownCommands(t *testing.T) {
	for _, arguments := range [][]string{{"unknown"}, {"release"}, {"release", "unknown"}, {"release", "build"}, {"check", "unexpected"}, {"release", "build", "--version", "v1.0.1", "--revision", "0000000000000000000000000000000000000000", "--race"}} {
		if err := run(t.Context(), arguments); err == nil {
			t.Fatalf("incomplete developer command accepted: %v", arguments)
		}
	}
}

// D3 (cmd/cwdev): offline evaluation is unchanged and needs no provider.
func TestEvalValidatesTheCheckedInCorpusOffline(t *testing.T) {
	if err := run(t.Context(), []string{"eval", "multilingual", "--source", "../.."}); err != nil {
		t.Fatal(err)
	}
}

// isolateEnvironment keeps eval away from the developer's own installation
// (R2.4): HOME is a fresh directory, PATH keeps only entries that hold no cw
// executable, and no adviser credential or host root leaks in, so a live
// evaluation can make no request.
func isolateEnvironment(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "TYPESAFE_API_KEY", "SUBQ_API_KEY", "OPENAI_API_KEY", "NO_PROXY", "no_proxy"} {
		t.Setenv(name, "")
	}
	// Defensive only: a transport that honours proxy variables cannot leave
	// this host even if a request were attempted.
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, "http://127.0.0.1:9")
	}
	var kept []string
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(entry, "cw")); err == nil {
			continue
		}
		kept = append(kept, entry)
	}
	t.Setenv("PATH", strings.Join(kept, string(os.PathListSeparator)))
	return home
}

func TestEvalArguments(t *testing.T) {
	isolateEnvironment(t)
	for _, arguments := range [][]string{{"eval"}, {"eval", "unknown"}} {
		if err := run(t.Context(), arguments); err == nil {
			t.Fatalf("accepted %v", arguments)
		}
	}
	for _, arguments := range [][]string{{"eval", "multilingual", "--unknown"}, {"eval", "multilingual", "unexpected"}, {"eval", "multilingual", "--live", "--source", "../.."}, {"eval", "multilingual", "--output-dir", "/unused", "--source", "../.."}} {
		if status, ok := errors.AsType[exitStatus](run(t.Context(), arguments)); !ok || status != 1 {
			t.Fatalf("%v: status %d, %v", arguments, status, ok)
		}
	}
}

// legacyRuntime places a cw whose runtime locator is version 2, the layout of
// a manager that predates the standalone release, and puts it first on PATH.
func legacyRuntime(t *testing.T, home string) {
	t.Helper()
	runtimeRoot := filepath.Join(home, ".local", "state", "agent-workflows", "runtime")
	release := filepath.Join(runtimeRoot, "releases", strings.Repeat("a", 40))
	if err := os.MkdirAll(release, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "cw"), []byte("#!/bin/sh\nexit 97\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	locator := fmt.Sprintf(`{"version":2,"source":%q,"home":%q,"state":%q,"command_path":%q}`,
		filepath.Join(home, "checkout"), home, filepath.Dir(runtimeRoot), filepath.Join(home, ".local", "bin", "cw"))
	path := filepath.Join(runtimeRoot, "locator.json")
	if err := os.WriteFile(path, []byte(locator), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", release+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// captureOutput returns what the action wrote to the process's standard
// streams.
func captureOutput(t *testing.T, action func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = file, file
	func() {
		defer func() { os.Stdout, os.Stderr = stdout, stderr }()
		action()
	}()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// R2.4: eval multilingual --live treats a legacy installation found through
// PATH exactly like no installation; it neither refuses nor fails differently.
func TestEvalLiveTreatsLegacyInstallationLikeNoInstallation(t *testing.T) {
	outcome := func(t *testing.T, legacy bool) (string, string) {
		t.Helper()
		home := isolateEnvironment(t)
		if legacy {
			legacyRuntime(t, home)
		}
		output := filepath.Join(t.TempDir(), "evaluation")
		var err error
		text := captureOutput(t, func() {
			err = run(t.Context(), []string{"eval", "multilingual", "--live", "--source", "../..", "--output-dir", output})
		})
		var result string
		switch status, ok := errors.AsType[exitStatus](err); {
		case err == nil:
			result = "ok"
		case ok:
			result = fmt.Sprintf("status %d", int(status))
		default:
			result = "error: " + strings.NewReplacer(output, "<output>", home, "<home>").Replace(err.Error())
		}
		return result, text
	}
	var baseline, baselineText, legacy, legacyText string
	t.Run("no_installation", func(t *testing.T) { baseline, baselineText = outcome(t, false) })
	t.Run("legacy_installation", func(t *testing.T) { legacy, legacyText = outcome(t, true) })
	if baseline == "" || legacy == "" {
		t.Fatal("an evaluation did not run")
	}
	if legacy != baseline {
		t.Fatalf("legacy installation changed the outcome: %s, without one: %s\nlegacy output: %s", legacy, baseline, legacyText)
	}
	// D3: with no configured provider a live evaluation is not a failure.
	if baseline != "ok" {
		t.Fatalf("live evaluation without configured providers failed: %s\noutput: %s", baseline, baselineText)
	}
	if strings.Contains(legacyText, "no longer supports") {
		t.Fatalf("legacy installation was refused: %s", legacyText)
	}
}

// evalTestNotRunEntry reports whether entry marks a provider as not run because it is
// not configured.
func evalTestNotRunEntry(entry map[string]any) bool {
	return entry["status"] == "not_run" && entry["reason"] == "not_configured"
}

// evalTestReportsNotRun finds provider marked not_run/not_configured anywhere in a
// decoded report, either keyed by the provider ID or naming it in a
// provider, id or name member.
func evalTestReportsNotRun(value any, provider string) bool {
	switch item := value.(type) {
	case map[string]any:
		if entry, ok := item[provider].(map[string]any); ok && evalTestNotRunEntry(entry) {
			return true
		}
		for _, key := range []string{"provider", "id", "name"} {
			if item[key] == provider && evalTestNotRunEntry(item) {
				return true
			}
		}
		for _, child := range item {
			if evalTestReportsNotRun(child, provider) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if evalTestReportsNotRun(child, provider) {
				return true
			}
		}
	}
	return false
}

// evalTestDecodedReports returns every JSON document the evaluation wrote below
// output (.json files and .jsonl lines) and any JSON object it printed.
func evalTestDecodedReports(t *testing.T, output, printed string) []any {
	t.Helper()
	var documents []any
	decode := func(data []byte) {
		var document any
		if json.Unmarshal(data, &document) == nil {
			documents = append(documents, document)
		}
	}
	err := filepath.WalkDir(output, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		switch filepath.Ext(path) {
		case ".json":
			decode(data)
		case ".jsonl":
			for line := range strings.SplitSeq(string(data), "\n") {
				decode([]byte(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	decode([]byte(printed))
	return documents
}

// D3 (cmd/cwdev multilingual eval): a live evaluation without any configured
// provider makes no request, is not a failure, and reports every known
// provider as not_run with reason not_configured.
func TestEvalLiveWithoutConfiguredProvidersReportsEveryProviderNotRun(t *testing.T) {
	isolateEnvironment(t)
	output := filepath.Join(t.TempDir(), "evaluation")
	var err error
	text := captureOutput(t, func() {
		err = run(t.Context(), []string{"eval", "multilingual", "--live", "--source", "../..", "--output-dir", output})
	})
	if err != nil {
		t.Fatalf("live evaluation without configured providers failed: %v\noutput: %s", err, text)
	}
	documents := evalTestDecodedReports(t, output, text)
	if len(documents) == 0 {
		t.Fatalf("live evaluation wrote no JSON report below %s; output: %s", output, text)
	}
	for _, provider := range []string{"jev", "subq_snap", "openai_decisions"} {
		if !slices.ContainsFunc(documents, func(document any) bool { return evalTestReportsNotRun(document, provider) }) {
			t.Fatalf("report does not list %s as not_run/not_configured; output: %s", provider, text)
		}
	}
}
