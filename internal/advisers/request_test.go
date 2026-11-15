package advisers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRequestAcceptsStructuredAdviceAndDecisions(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "advice", text: `{"version":1,"kind":"advice","question":"What should change?"}`},
		{name: "advice with no choices", text: `{"version":1,"kind":"advice","question":"What should change?","choices":[]}`},
		{name: "decision", text: `{"version":1,"kind":"decision","question":"Which option?","choices":[{"id":"a","text":"Keep it"},{"id":"b","text":"Змінити"}],"context":["docs/design.md"]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseRequest(strings.NewReader(test.text))
			if err != nil {
				t.Fatal(err)
			}
			if got.Version != 1 || got.Question == "" {
				t.Fatalf("request = %#v", got)
			}
		})
	}
}

func TestParseRequestRejectsAmbiguousOrInvalidDocuments(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "empty", text: ""},
		{name: "null", text: "null"},
		{name: "array", text: "[]"},
		{name: "missing version", text: `{"kind":"advice","question":"Why?"}`},
		{name: "future version", text: `{"version":3,"kind":"advice","question":"Why?"}`},
		{name: "unknown kind", text: `{"version":1,"kind":"run","question":"Why?"}`},
		{name: "blank question", text: `{"version":1,"kind":"advice","question":" \n\t"}`},
		{name: "unknown field", text: `{"version":1,"kind":"advice","question":"Why?","prompt":"different"}`},
		{name: "capitalized field", text: `{"version":1,"kind":"advice","Question":"Why?"}`},
		{name: "two documents", text: `{"version":1,"kind":"advice","question":"Why?"} {}`},
		{name: "trailing garbage", text: `{"version":1,"kind":"advice","question":"Why?"} tail`},
		{name: "duplicate field", text: `{"version":1,"kind":"advice","question":"First?","question":"Second?"}`},
		{name: "escaped duplicate", text: `{"version":1,"kind":"advice","question":"First?","quest\u0069on":"Second?"}`},
		{name: "case duplicate", text: `{"version":1,"kind":"advice","question":"First?","Question":"Second?"}`},
		{name: "advice choices", text: `{"version":1,"kind":"advice","question":"Why?","choices":[{"id":"a","text":"A"},{"id":"b","text":"B"}]}`},
		{name: "missing decision choices", text: `{"version":1,"kind":"decision","question":"Which?"}`},
		{name: "one decision choice", text: `{"version":1,"kind":"decision","question":"Which?","choices":[{"id":"a","text":"A"}]}`},
		{name: "duplicate choice id", text: `{"version":1,"kind":"decision","question":"Which?","choices":[{"id":"a","text":"A"},{"id":"a","text":"B"}]}`},
		{name: "blank choice id", text: `{"version":1,"kind":"decision","question":"Which?","choices":[{"id":" ","text":"A"},{"id":"b","text":"B"}]}`},
		{name: "blank choice text", text: `{"version":1,"kind":"decision","question":"Which?","choices":[{"id":"a","text":" "},{"id":"b","text":"B"}]}`},
		{name: "unknown choice field", text: `{"version":1,"kind":"decision","question":"Which?","choices":[{"id":"a","text":"A","score":1},{"id":"b","text":"B"}]}`},
		{name: "duplicate nested field", text: `{"version":1,"kind":"decision","question":"Which?","choices":[{"id":"a","id":"c","text":"A"},{"id":"b","text":"B"}]}`},
		{name: "wrong context type", text: `{"version":1,"kind":"advice","question":"Why?","context":"file.md"}`},
		{name: "duplicate context", text: `{"version":1,"kind":"advice","question":"Why?","context":["file.md","file.md"]}`},
		{name: "invalid UTF-8", text: "{\"version\":1,\"kind\":\"advice\",\"question\":\"\xff\"}"},
		{name: "oversize input", text: strings.Repeat(" ", MaxInput+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseRequest(strings.NewReader(test.text)); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
}

type failingAdviserRequestReader struct{}

func (failingAdviserRequestReader) Read([]byte) (int, error) {
	return 0, errors.New("request read failed")
}

func TestParseRequestReportsReaderFailure(t *testing.T) {
	if _, err := ParseRequest(failingAdviserRequestReader{}); err == nil || !strings.Contains(err.Error(), "request read failed") {
		t.Fatalf("request error = %v", err)
	}
}

func TestValidateRequestRejectsUnsafeContextPaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../secret", "/absolute", "a/../b", "./file", "a//b", "a/", "a\\b", "C:/file", "file\x00", "\xff"} {
		t.Run(name, func(t *testing.T) {
			request := Request{Version: 1, Kind: "advice", Question: "Why?", Context: []string{name}}
			if err := ValidateRequest(request); err == nil {
				t.Fatalf("unsafe context path %q was accepted", name)
			}
		})
	}
	request := Request{Version: 1, Kind: "advice", Question: strings.Repeat("q", MaxInput)}
	if err := ValidateRequest(request); err == nil {
		t.Fatal("oversize structured request was accepted")
	}
	request.Question = "\xff"
	if err := ValidateRequest(request); err == nil {
		t.Fatal("invalid question UTF-8 was accepted through the Go API")
	}
}

func TestRenderIncludesOnlyExplicitContextAndPreservesChoiceOrder(t *testing.T) {
	root := t.TempDir()
	writeAdviserTestFile(t, filepath.Join(root, "context.md"), "Evidence: Привіт!\n")
	writeAdviserTestFile(t, filepath.Join(root, "secret.md"), "do-not-send-unlisted-context")
	request := Request{
		Version: 1, Kind: "decision", Question: "Which direction?",
		Choices: []Choice{{ID: "second", Text: "Keep it"}, {ID: "first", Text: "Change it"}},
		Context: []string{"context.md"},
	}
	got, err := Render(root, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"coordinator decides", "Request kind: decision", request.Question, "Evidence: Привіт!\n", `Context file "context.md"`} {
		if !strings.Contains(got, wanted) {
			t.Errorf("rendered request lacks %q", wanted)
		}
	}
	if strings.Index(got, `"second": Keep it`) > strings.Index(got, `"first": Change it`) {
		t.Fatal("choice order changed")
	}
	if strings.Contains(got, "do-not-send-unlisted-context") {
		t.Fatal("an unlisted file was included")
	}
}

func TestRenderRejectsUnreadableBinaryDirectoryAndOversizeContext(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		dir     bool
	}{
		{name: "missing file", path: "missing.md"},
		{name: "directory", path: "directory", dir: true},
		{name: "binary", path: "binary", content: "\xff"},
		{name: "oversize file", path: "large", content: strings.Repeat("x", MaxInput+1)},
		{name: "format overhead", path: "almost", content: strings.Repeat("x", MaxInput-210)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, test.path)
			switch {
			case test.dir:
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case test.content != "":
				writeAdviserTestFile(t, path, test.content)
			}
			request := Request{Version: 1, Kind: "advice", Question: "Why?", Context: []string{test.path}}
			if _, err := Render(root, request); err == nil {
				t.Fatal("unusable context was rendered")
			}
		})
	}
}

func TestRenderConfinesSymlinksToRepository(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	writeAdviserTestFile(t, outside, "outside evidence")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	request := Request{Version: 1, Kind: "advice", Question: "Why?", Context: []string{"escape"}}
	if _, err := Render(root, request); err == nil {
		t.Fatal("an escaping symlink was read")
	}
	writeAdviserTestFile(t, filepath.Join(root, "inside"), "inside evidence")
	if err := os.Symlink("inside", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	request.Context = []string{"link"}
	got, err := Render(root, request)
	if err != nil || !strings.Contains(got, "inside evidence") {
		t.Fatalf("internal symlink: rendered %q, error %v", got, err)
	}
}
