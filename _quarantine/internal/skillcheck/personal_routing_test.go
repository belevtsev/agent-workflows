package skillcheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersonalRoutingFixturesKeepExpectedAnswersSeparate(t *testing.T) {
	root := filepath.Join("testdata", "personal-skill-routing")
	fixtures, err := decodeRoutingFixture[routingFixtures](filepath.Join(root, "oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRoutingFixtures(filepath.Join("..", ".."), root, fixtures); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalRoutingNestedInputBoundaries(t *testing.T) {
	fixtures, err := decodeRoutingFixture[routingFixtures](filepath.Join("testdata", "personal-skill-routing", "oracles.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixtures.Cases = fixtures.Cases[:1]
	for _, test := range []struct {
		name     string
		artifact string
		link     bool
		linkDir  bool
		wantPass bool
	}{
		{name: "nested project file", artifact: "project/openspec/config.yaml", wantPass: true},
		{name: "nested evaluator file", artifact: "project/notes/oracles.json"},
		{name: "nested evaluator directory", artifact: "project/expected/config.yaml"},
		{name: "nested file link", artifact: "project/openspec/config.yaml", link: true},
		{name: "nested directory link", artifact: "project/openspec", link: true, linkDir: true},
		{name: "empty project tree", artifact: "project/openspec", linkDir: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "inputs", "planning")
			if err := os.MkdirAll(input, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(input, "prompt.md"), []byte("Prepare a rollout plan from these local files.\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(input, filepath.FromSlash(test.artifact))
			if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
				t.Fatal(err)
			}
			if test.link {
				target := t.TempDir()
				if !test.linkDir {
					target = filepath.Join(target, "config.yaml")
					if err := os.WriteFile(target, []byte("schema: spec-driven\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, artifact); err != nil {
					t.Fatal(err)
				}
			} else if test.linkDir {
				if err := os.MkdirAll(artifact, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(artifact, []byte("schema: spec-driven\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := validateRoutingFixtures(filepath.Join("..", ".."), root, fixtures)
			if (err == nil) != test.wantPass {
				t.Fatalf("nested input validation: err=%v, want pass=%v", err, test.wantPass)
			}
		})
	}
}
