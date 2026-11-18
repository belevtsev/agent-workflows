package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/store"
)

func TestSourceChangeSummaryIsBoundedAndSingleLine(t *testing.T) {
	detail := "changed\n" + strings.Repeat("試", 250)
	short := sourceChangeSummary(store.Record{Outcome: store.SourceChanged, Detail: detail})
	if !utf8.ValidString(short) || utf8.RuneCountInString(short) != 240 || strings.Contains(short, "\n") || !strings.HasSuffix(short, "…") {
		t.Fatalf("bad terminal summary: %q", short)
	}
	if summary := sourceChangeSummary(store.Record{Outcome: store.Passed, Detail: detail}); summary != "" {
		t.Fatal("successful verification repeated diagnostics")
	}
}
