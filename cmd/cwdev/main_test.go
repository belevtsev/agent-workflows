package main

import (
	"errors"
	"testing"
)

func TestDeveloperArgumentsRejectIncompleteOrUnknownCommands(t *testing.T) {
	for _, arguments := range [][]string{{"unknown"}, {"release"}, {"release", "unknown"}, {"release", "build"}, {"check", "unexpected"}, {"release", "build", "--version", "v1.0.1", "--revision", "0000000000000000000000000000000000000000", "--race"}} {
		if err := run(t.Context(), arguments); err == nil {
			t.Fatalf("incomplete developer command accepted: %v", arguments)
		}
	}
}

func TestEvalValidatesTheCheckedInCorpusOffline(t *testing.T) {
	if err := run(t.Context(), []string{"eval", "multilingual", "--source", "../.."}); err != nil {
		t.Fatal(err)
	}
}

func TestEvalArguments(t *testing.T) {
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
