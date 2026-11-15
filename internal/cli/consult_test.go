package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func TestConsultationPreflightRefusesChangedEvidenceBeforeRecordingOrCalling(t *testing.T) {
	ctx := context.Background()
	repo, root := guardRepo(t, "")
	s := &session{repo: repo, store: store.Open(repo.CommonDir), cfg: Config{Out: io.Discard, Err: io.Discard}}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "provider-called")
	t.Setenv("KITSU_PREFLIGHT_PROVIDER_MARKER", marker)
	definition := advisers.Definition{
		Model: "fake-model", Timeout: time.Minute,
		Command: []string{program, "-test.run=^TestConsultationPreflightProviderHelper$", "--", "{model}"},
	}
	configuration := &advisers.Config{
		Advisers: map[string]advisers.Definition{"local": definition}, Access: map[string][]string{"coordinator": {"local"}},
	}
	if _, err := advisers.Bind(root, "preflight-task", "small", configuration, false); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("original evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	baseline, _, err := s.adviserContent(ctx, "preflight-task", false)
	if err != nil {
		t.Fatal(err)
	}
	request := advisers.Request{Version: 1, Kind: "advice", Question: "What could fail?", Context: []string{"source.txt"}}
	prompt, err := advisers.Render(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	consultation := advisers.Consultation{
		Task: "preflight-task", Role: "coordinator", Stage: "code", Adviser: "local",
		Content: baseline, Request: request, Prompt: prompt,
	}
	err = s.executeConsultation(ctx, consultation, definition, false)
	if err == nil || !strings.Contains(err.Error(), "changed before execution") {
		t.Fatalf("changed evidence was accepted: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the adviser ran for changed evidence: %v", err)
	}
	records, err := advisers.ReadConsultations(root, consultation.Task)
	if err != nil || len(records) != 0 {
		t.Fatalf("preflight rejection left an attempt: %+v, %v", records, err)
	}
	location, err := advisers.Location(root, consultation.Task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(location), "exchanges")); !os.IsNotExist(err) {
		t.Fatalf("preflight rejection created pending history: %v", err)
	}

	// A fresh request must reach the same provider, so the earlier rejection
	// cannot pass merely because execution is disabled or the fake is broken.
	consultation.Content, _, err = s.adviserContent(ctx, consultation.Task, false)
	if err != nil {
		t.Fatal(err)
	}
	consultation.Prompt, err = advisers.Render(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.executeConsultation(ctx, consultation, definition, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the fresh consultation did not reach the provider: %v", err)
	}
	records, err = advisers.ReadConsultations(root, consultation.Task)
	if err != nil || len(records) != 1 || records[0].Execution.Outcome != "success" {
		t.Fatalf("fresh consultation was not retained: %+v, %v", records, err)
	}
}

func TestConsultationPreflightProviderHelper(t *testing.T) {
	marker := os.Getenv("KITSU_PREFLIGHT_PROVIDER_MARKER")
	if marker == "" || os.Getenv("KITSU_ADVISER_CALL") != "1" {
		return
	}
	if err := os.WriteFile(marker, []byte("called"), 0o600); err != nil {
		os.Exit(91)
	}
	fmt.Fprint(os.Stdout, "Advice for the current evidence.")
	os.Exit(0)
}
