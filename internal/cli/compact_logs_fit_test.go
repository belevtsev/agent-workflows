package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

func compactFitCheckChild(mode string) {
	f, err := os.OpenFile(os.Getenv("KITSU_TEST_COMPACT_READY"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(9)
	}
	_, _ = f.WriteString("run\n")
	_ = f.Close()
	mode = strings.TrimPrefix(mode, "bounded-")
	text := ""
	switch mode {
	case "quoted":
		text = strings.Repeat(`"`, 10000)
	case "invalid":
		text = strings.Repeat("☃", 500) + strings.Repeat(string([]byte{0x80}), 100) + "\nIMPORTANT invalid bytes\n"
	default:
		count, _ := strconv.Atoi(mode)
		text = strings.Repeat("diagnostic", count*1024/10) + strings.Repeat("x", count*1024%10)
	}
	fmt.Print(text)
	os.Exit(7)
}

func compactItemText(item contextpack.PageItem) string {
	var text strings.Builder
	for _, line := range item.Lines {
		text.WriteString(line.Text)
	}
	return text.String()
}

func TestCompactFittingFailuresAreCompleteAndExpansionNeverReruns(t *testing.T) {
	for _, mode := range []string{"1", "4", "5", "6", "invalid", "quoted", "100"} {
		t.Run(mode, func(t *testing.T) {
			r := compactChecksFixture(t, "bounded-"+mode)
			page := compactResult(t, r.Kitsu("check", "run", "--compact"), 5)
			var meta compactCheckMeta
			if err := json.Unmarshal(page.Metadata, &meta); err != nil {
				t.Fatal(err)
			}
			record := meta.Checks[0].Record
			if record == nil || record.ExitStatus == nil || *record.ExitStatus != 7 || meta.Verified {
				t.Fatal("failure outcome was lost", meta)
			}
			original := r.Read(".git/kitsu/" + record.Log)
			item := page.Items[0]
			large := mode == "quoted" || mode == "100"
			if large {
				if !item.Preview || item.Omitted == 0 || item.Next == "" {
					t.Fatal("oversize encoded evidence claimed complete", item)
				}
			} else if item.Preview || item.Omitted != 0 || item.Next != "" || compactItemText(item) != strings.ToValidUTF8(original, "\ufffd") {
				t.Fatal("fitting evidence required expansion or changed order", item)
			}
			args := []string{"context", "show", page.ID, "--item", item.ID, "--max-bytes", "1048576"}
			if item.Next != "" {
				args = append(args, "--cursor", item.Next)
			}
			expansion := r.Kitsu(args...)
			var full contextpack.Page
			if expansion.Code != 0 || json.Unmarshal([]byte(expansion.Stdout), &full) != nil {
				t.Fatal(expansion.Output())
			}
			if full.Items[0].Omitted != 0 || compactItemText(full.Items[0]) != strings.ToValidUTF8(original, "\ufffd") {
				t.Fatal("original retained log was not recovered")
			}
			if got := r.Read(".git/sequence-ready"); got != "run\n" {
				t.Fatalf("log observation reran the check: %q", got)
			}
		})
	}
}

func TestCompactLogCandidateMatchesOriginalChunking(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, text string }{
		{"empty", ""},
		{"six_chunks", strings.Repeat("row\n", 6*256)},
		{"utf8_boundary", strings.Repeat("x", 1023) + strings.Repeat("☃\n", 1500)},
		{"invalid_boundary", strings.Repeat("x", 1023) + strings.Repeat(string([]byte{0x80}), 9000) + "\n" + strings.Repeat("y", 3000)},
		{"valid_replacement", strings.Repeat("\ufffd", 2000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, err := collectLog("retained.log", strings.NewReader(tc.text))
			if err != nil {
				t.Fatal(err)
			}
			for _, budget := range []int{0, 1024, contextpack.DefaultBytes} {
				got, total, err := collectLogCandidate("retained.log", strings.NewReader(tc.text), budget)
				if err != nil || total != len(original) {
					t.Fatalf("candidate changed log total: %d, %v", total, err)
				}
				want := original
				if len(original) > 4 && compactLogBytes(original) > budget {
					want = append(append([]contextpack.Line{}, original[len(original)-3:]...), original[0])
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("budget %d changed log chunks or anchors", budget)
				}
			}
		})
	}
}

func TestCompactMultipleFailuresShareOneInitialBudget(t *testing.T) {
	for _, tc := range []struct {
		name  string
		modes []string
		fits  bool
	}{
		{"complete", []string{"bounded-6", "bounded-5"}, true},
		{"preview", []string{"bounded-6", "bounded-5", "bounded-4"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compactChecksFixture(t, tc.modes...)
			page := compactResult(t, r.Kitsu("check", "run", "--compact"), 5)
			omitted := 0
			for _, item := range page.Items {
				omitted += item.Omitted
				if item.Omitted > 0 && (!item.Preview || item.Next == "") {
					t.Fatal("omitted evidence lost original-log continuation", item)
				}
			}
			if (omitted == 0) != tc.fits {
				t.Fatalf("shared response budget fit=%t, want %t", omitted == 0, tc.fits)
			}
			if got := r.Read(".git/sequence-ready"); got != strings.Repeat("run\n", len(tc.modes)) {
				t.Fatal("checks did not execute exactly once", got)
			}
		})
	}
}

type oversizedLogReader struct{ bytes int }

func (r *oversizedLogReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.bytes += len(p)
	return len(p), nil
}

func TestCompactLogCandidateStopsAtRetainedLimit(t *testing.T) {
	t.Parallel()
	reader := &oversizedLogReader{}
	if _, _, err := collectLogCandidate("too-large", reader, 512); err == nil {
		t.Fatal("unbounded log was accepted")
	}
	if reader.bytes != contextpack.MaxArtifact+1 {
		t.Fatal("log reader exceeded its bound", reader.bytes)
	}
	if _, _, err := collectLogCandidate("unreadable", io.MultiReader(strings.NewReader("prefix"), failingLogReader{}), 512); err == nil {
		t.Fatal("read failure was represented as complete evidence")
	}
}

type failingLogReader struct{}

func (failingLogReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("unreadable retained log")
}
