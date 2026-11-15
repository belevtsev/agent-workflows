package repocheck

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// diagramFolder holds the diagrams: each is a JSON spec of archify with its
// SVG export beside it.
const diagramFolder = "docs/diagrams/"

// shownAsImage is how a document shows a diagram.
const shownAsImage = `<img src="..."> or ![...](...)`

// diagramProblems checks the diagrams among files, which are paths under
// root with forward slashes. A spec needs its export, which must hold what
// the spec holds; an export needs its spec and a Markdown file that shows it
// as an image; and the folder holds nothing else.
func diagramProblems(root string, files []string) []string {
	var problems []string
	shown := map[string]bool{}
	for _, file := range files {
		if !strings.HasSuffix(file, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, target := range markdownImages(data) {
			if link, err := url.Parse(strings.Trim(target, "<>")); err == nil && link.Scheme == "" && link.Host == "" {
				shown[path.Join(path.Dir(file), link.Path)] = true
			}
		}
	}
	for _, file := range files {
		if !strings.HasPrefix(file, diagramFolder) {
			continue
		}
		name := strings.TrimSuffix(file, path.Ext(file))
		switch path.Ext(file) {
		case ".json":
			if slices.Contains(files, name+".svg") {
				problems = append(problems, exportProblems(root, file, name+".svg")...)
			} else {
				problems = append(problems, fmt.Sprintf("%s: the export %s is not beside it", file, name+".svg"))
			}
		case ".svg":
			if !slices.Contains(files, name+".json") {
				problems = append(problems, fmt.Sprintf("%s: the spec %s is not beside it", file, name+".json"))
			}
			if !shown[file] {
				problems = append(problems, file+": no Markdown file shows it as an image, with "+shownAsImage)
			}
		default:
			problems = append(problems, file+": a diagram is a .json spec and its .svg export, and nothing else")
		}
	}
	return problems
}

func TestEveryDiagramHasItsSpecItsExportAndAPlace(t *testing.T) {
	files := repoFiles(t)
	if !slices.ContainsFunc(files, func(file string) bool { return strings.HasPrefix(file, diagramFolder) }) {
		t.Errorf("%s holds no diagram", diagramFolder)
	}
	for _, problem := range diagramProblems(repoRoot, files) {
		t.Error(problem)
	}
}

func TestDiagramProblemsAreFound(t *testing.T) {
	const (
		specFile   = "docs/diagrams/a.json"
		exportFile = "docs/diagrams/a.svg"
		page       = "docs/a.md"
		image      = "![one box](diagrams/a.svg)"
		notShown   = exportFile + ": no Markdown file shows it as an image, with " + shownAsImage
		spec       = `{"meta": {"title": "One box", "legend": {"mode": "hidden"}}, "components": [{"id": "a", "type": "backend", "label": "a"}]}`
		export     = `<svg xmlns="http://www.w3.org/2000/svg"><title id="archify-diagram-title">One box</title>
			<g data-node-id="a" data-node-label="a" data-node-kind="backend" data-node-context="Architecture"><g data-semantic-sigil="backend"/></g></svg>`
	)
	for _, test := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name:  "a Markdown image",
			files: map[string]string{specFile: spec, exportFile: export, page: image},
		},
		{
			name:  "an HTML image",
			files: map[string]string{specFile: spec, exportFile: export, "README.md": `<img src="docs/diagrams/a.svg" alt="one box" width="100%">`},
		},
		{
			name:  "an HTML image in single quotes, on two lines",
			files: map[string]string{specFile: spec, exportFile: export, page: "<img alt='one box'\n     src='diagrams/a.svg'>"},
		},
		{
			name:  "an HTML image without quotes",
			files: map[string]string{specFile: spec, exportFile: export, page: "<img src=diagrams/a.svg>"},
		},
		{
			name:  "an image that names a definition",
			files: map[string]string{specFile: spec, exportFile: export, page: "![one box][The Picture]\n\n[the picture]: diagrams/a.svg"},
		},
		{
			name:  "a spec without its export",
			files: map[string]string{specFile: spec},
			want:  []string{specFile + ": the export " + exportFile + " is not beside it"},
		},
		{
			name:  "an export without its spec",
			files: map[string]string{exportFile: export, "README.md": "![one box](docs/diagrams/a.svg)"},
			want:  []string{exportFile + ": the spec " + specFile + " is not beside it"},
		},
		{
			name:  "an image inside a code span",
			files: map[string]string{specFile: spec, exportFile: export, page: "`" + image + "`"},
			want:  []string{notShown},
		},
		{
			name:  "an image inside an HTML comment",
			files: map[string]string{specFile: spec, exportFile: export, page: "<!--\n" + image + "\n-->"},
			want:  []string{notShown},
		},
		{
			name:  "a link in place of an image",
			files: map[string]string{specFile: spec, exportFile: export, page: "[one box](diagrams/a.svg)"},
			want:  []string{notShown},
		},
		{
			name:  "a source in another format",
			files: map[string]string{"docs/diagrams/a.mmd": "graph TD"},
			want:  []string{"docs/diagrams/a.mmd: a diagram is a .json spec and its .svg export, and nothing else"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			var files []string
			for file, content := range test.files {
				writeFixture(t, filepath.Join(root, filepath.FromSlash(file)), []byte(content))
				files = append(files, file)
			}
			slices.Sort(files)
			if got := diagramProblems(root, files); !slices.Equal(got, test.want) {
				t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(test.want, "\n"))
			}
		})
	}
}
