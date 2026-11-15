package declaration_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// The scenarios in this file come from the second challenge of 14 November
// 2026 (openspec/production-plan.md, Independent challenge).

// declarationWith is a kitsu.toml of one check `p` that runs git --version, with
// the given TOML lines added to the check.
func declarationWith(lines string) string {
	return "version = 1\n\n[[check]]\nid = \"p\"\nrun = [\"git\", \"--version\"]\n" + lines
}

// Requirement "Check definition", scenario "A device name of Windows is
// refused".
func TestDeviceNamesOfWindowsAreRefusedAsIDs(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	for _, id := range []string{"con", "prn", "aux", "nul", "com1", "lpt9"} {
		repo.Write("kitsu.toml", "version = 1\n\n[[check]]\nid = \""+id+"\"\nrun = [\"git\", \"--version\"]\n")
		result := repo.Kitsu("status")
		if result.Code != 1 {
			t.Errorf("id %s: kitsu status exited %d, want 1", id, result.Code)
		}
		for _, want := range []string{"kitsu.toml:4", "`" + id + "`", "Windows"} {
			if !strings.Contains(result.Output(), want) {
				t.Errorf("id %s: the output of kitsu status does not hold %q:\n%s", id, want, result.Output())
			}
		}
	}

	repo.Git("checkout", "-q", "--", "kitsu.toml")
	sum := sha256File(repo, "kitsu.toml")
	result := repo.Kitsu("add", "aux", "--", "git", "--version")
	if result.Code != 1 || !strings.Contains(result.Output(), "Windows") {
		t.Errorf("kitsu add aux exited %d, want 1 and a reason that names Windows:\n%s", result.Code, result.Output())
	}
	if after := sha256File(repo, "kitsu.toml"); after != sum {
		t.Errorf("kitsu add aux changed kitsu.toml:\n%s", repo.Read("kitsu.toml"))
	}
}

// Requirement "Invalid declaration", scenario "A name with a control
// character is printed quoted".
func TestANameWithAControlCharacterIsPrintedQuoted(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	// A quoted TOML key that holds ESC [ 2 J, which clears a terminal.
	repo.Write("kitsu.toml", kitsutest.DemoDeclaration+"\"x\\u001b[2Jy\" = 1\n")

	result := repo.Kitsu("status")
	if result.Code != 1 {
		t.Errorf("kitsu status exited %d, want 1", result.Code)
	}
	if strings.ContainsRune(result.Output(), 0x1b) {
		t.Errorf("the output holds the control character ESC: %q", result.Output())
	}
	for _, want := range []string{"unknown key", `\x1b[2J`} {
		if !strings.Contains(result.Output(), want) {
			t.Errorf("the output does not hold %q: %q", want, result.Output())
		}
	}
}

// Requirement "Invalid declaration", scenario "A pattern that names a folder
// is refused".
func TestAPatternThatNamesAFolderIsRefused(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	repo.Write("kitsu.toml", declarationWith("paths = [\"src\"]\n"))
	commitAll(repo, "paths names a folder")
	wantRefusal := func(what string, result kitsutest.Result) {
		t.Helper()
		if result.Code == 0 {
			t.Errorf("%s exited 0, want a refusal", what)
		}
		for _, want := range []string{"kitsu.toml:3", "`src`", "names a folder", "`src/**`"} {
			if !strings.Contains(result.Output(), want) {
				t.Errorf("the output of %s does not hold %q:\n%s", what, want, result.Output())
			}
		}
	}

	before := fingerprint(t, repo.Dir, repo.Home)
	run := repo.Kitsu("run")
	wantRefusal("kitsu run", run)
	if run.Code != 1 {
		t.Errorf("kitsu run exited %d, want 1", run.Code)
	}
	if strings.Contains(run.Output(), "git version") {
		t.Errorf("kitsu run executed the check:\n%s", run.Output())
	}
	assertWroteNothing(t, repo, before)

	status := repo.Kitsu("status")
	wantRefusal("kitsu status", status)
	if status.Code != 1 {
		t.Errorf("kitsu status exited %d, want 1", status.Code)
	}

	if result := repo.Kitsu("init"); result.Code != 0 {
		t.Fatalf("kitsu init exited %d:\n%s", result.Code, result.Output())
	}
	head := repo.Git("rev-parse", "HEAD")
	repo.Write("README.md", "# demo, edited\n")
	wantRefusal("git commit", repo.TryGit("commit", "-a", "-m", "change"))
	if after := repo.Git("rev-parse", "HEAD"); after != head {
		t.Errorf("git commit recorded a commit: HEAD moved from %s to %s", head, after)
	}
}

// Requirement "Add a check from the command line", scenario "The program
// goes after `--`".
func TestAddWithoutTheDashesSaysWhereTheProgramGoes(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	sum := sha256File(repo, "kitsu.toml")

	result := repo.Kitsu("add", "unit2", "go", "test", "-v", "./...")
	if result.Code != 2 {
		t.Errorf("kitsu add exited %d, want 2", result.Code)
	}
	if !strings.Contains(result.Output(), "the program and its arguments go after `--`") {
		t.Errorf("the output does not say where the program goes:\n%s", result.Output())
	}
	if after := sha256File(repo, "kitsu.toml"); after != sum {
		t.Errorf("kitsu add changed kitsu.toml:\n%s", repo.Read("kitsu.toml"))
	}
}

// Requirement "Add a check from the command line", scenario "A command
// written as one word is refused".
func TestAddRefusesACommandWrittenAsOneWord(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	sum := sha256File(repo, "kitsu.toml")
	before := fingerprint(t, repo.Dir, repo.Home)

	result := repo.Kitsu("add", "unit2", "--", "go test ./...")
	if result.Code != 2 {
		t.Errorf("kitsu add exited %d, want 2", result.Code)
	}
	if !strings.Contains(result.Output(), "one word with a space") {
		t.Errorf("the output does not say that the program is one word with a space:\n%s", result.Output())
	}
	if after := sha256File(repo, "kitsu.toml"); after != sum {
		t.Errorf("kitsu add changed kitsu.toml:\n%s", repo.Read("kitsu.toml"))
	}
	assertWroteNothing(t, repo, before)

	// A program whose path holds a space, and that exists, is accepted.
	program := "my tools/check.sh"
	if runtime.GOOS == "windows" {
		program = "my tools/check.bat"
	}
	writeProgram(t, repo.Path(program), "exit 0\n")
	given := "." + string(filepath.Separator) + filepath.FromSlash(program)
	result = repo.Kitsu("add", "tools", "--", given, "--quick")
	if result.Code != 0 {
		t.Errorf("kitsu add of the program %q exited %d, want 0:\n%s", given, result.Code, result.Output())
	}
	if !strings.Contains(repo.Read("kitsu.toml"), "id = \"tools\"") {
		t.Errorf("kitsu.toml does not hold the check tools:\n%s", repo.Read("kitsu.toml"))
	}
}

// Requirement "Create a declaration", scenario "The next step is named".
func TestInitNamesTheNextStep(t *testing.T) {
	t.Parallel()
	demo := kitsutest.Demo(t)
	result := demo.Kitsu("init")
	if result.Code != 0 || !strings.Contains(result.Stdout, "next: kitsu run\n") {
		t.Errorf("kitsu init in demo exited %d and did not print the line `next: kitsu run`:\n%s", result.Code, result.Output())
	}
	if strings.Contains(result.Stdout, "next: kitsu add") {
		t.Errorf("kitsu init in demo, whose kitsu.toml declares checks, names kitsu add as the next step:\n%s", result.Output())
	}

	fresh := kitsutest.NewRepo(t)
	result = fresh.Kitsu("init")
	if result.Code != 0 || !strings.Contains(result.Stdout, "next: kitsu add <id> -- <program> [<argument>...], then kitsu run") {
		t.Errorf("kitsu init in a new repository exited %d and did not name kitsu add as the next step:\n%s", result.Code, result.Output())
	}
}
