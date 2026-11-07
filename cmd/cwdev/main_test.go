package main

import (
	json "encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDeveloperArgumentsRejectIncompleteOrUnknownCommands(t *testing.T) {
	for _, arguments := range [][]string{{"unknown"}, {"release"}, {"check"}, {"sandbox", "test"}} {
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
