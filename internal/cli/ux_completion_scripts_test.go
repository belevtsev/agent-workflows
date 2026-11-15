package cli

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUXCompletionGeneratedShellSyntaxAndSmoke(t *testing.T) {
	tests := []struct {
		name, program string
		syntaxArgs    []string
	}{
		{name: "bash", program: "bash", syntaxArgs: []string{"-n"}},
		{name: "zsh", program: "zsh", syntaxArgs: []string{"-n"}},
		{name: "fish", program: "fish", syntaxArgs: []string{"-n"}},
		{name: "powershell", program: "pwsh"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := exec.LookPath(test.program); err != nil {
				t.Skipf("%s is not installed", test.program)
			}
			repo, _ := skillRepo(t)
			result := repo.Kitsu("completion", test.name)
			if result.Code != 0 || result.Stderr != "" || !strings.Contains(result.Stdout, "__complete") {
				t.Fatalf("script generation failed: %s", result.Output())
			}
			filename := "generated-completion"
			if test.name == "powershell" {
				filename += ".ps1"
			}
			repo.Write(filename, result.Stdout)
			path := repo.Path(filename)
			if test.name == "powershell" {
				code := "$tokens = $null; $errors = $null; [System.Management.Automation.Language.Parser]::ParseFile(" + uxPowerShellTestQuote(path) + ", [ref]$tokens, [ref]$errors) > $null; if ($errors.Count) { $errors; exit 1 }"
				if parsed := repo.Run("", test.program, "-NoProfile", "-NonInteractive", "-Command", code); parsed.Code != 0 {
					t.Fatal(parsed.Output())
				}
			} else {
				args := append(append([]string{}, test.syntaxArgs...), path)
				if parsed := repo.Run("", test.program, args...); parsed.Code != 0 {
					t.Fatal(parsed.Output())
				}
			}
			var smoke string
			args := []string{"-c"}
			switch test.name {
			case "bash":
				smoke = ". " + uxShellTestQuote(path) + "\nCOMP_LINE='kitsu role text co'\nCOMP_POINT=${#COMP_LINE}\nCOMP_WORDS=(kitsu role text co)\nCOMP_CWORD=3\n_kitsu_completion\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
			case "zsh":
				smoke = "source " + uxShellTestQuote(path) + "\ncompadd() { shift; printf '%s\\n' \"$@\"; }\nwords=(kitsu role text co)\nCURRENT=4\n_kitsu_completion\n"
			case "fish":
				smoke = "source " + uxShellTestQuote(path) + "\ncomplete -C 'kitsu role text co'\n"
			case "powershell":
				args = []string{"-NoProfile", "-NonInteractive", "-Command"}
				smoke = ". " + uxPowerShellTestQuote(path) + "; (TabExpansion2 'kitsu role text co' 18).CompletionMatches.CompletionText"
			}
			if completed := repo.Run("", test.program, append(args, smoke)...); completed.Code != 0 || !strings.Contains(completed.Stdout, "coordinator") {
				t.Fatalf("completion smoke failed: %s", completed.Output())
			}
		})
	}
}

func TestUXCompletionZshAutoloadOnFirstQuery(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	repo, _ := skillRepo(t)
	generated := repo.Kitsu("completion", "zsh")
	if generated.Code != 0 {
		t.Fatal(generated.Output())
	}
	repo.Write("completion-functions/_kitsu", generated.Stdout)
	script := "fpath=(" + uxShellTestQuote(repo.Path("completion-functions")) + " $fpath)\nautoload -Uz _kitsu\ncompadd() { shift; printf '%s\\n' \"$@\"; }\nwords=(kitsu role text co)\nCURRENT=4\n_kitsu\n"
	if completed := repo.Run("", "zsh", "-c", script); completed.Code != 0 || strings.TrimSpace(completed.Stdout) != "coordinator" {
		t.Fatalf("first Zsh autoload query failed: %s", completed.Output())
	}
}

func TestUXCompletionPowerShellPreservesUnicodeAndQuotedValues(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh is not installed")
	}
	repo, _ := skillRepo(t)
	file := "привіт $(touch completion-executed).json"
	repo.Write(file, "{}")
	generated := repo.Kitsu("completion", "powershell")
	if generated.Code != 0 {
		t.Fatal(generated.Output())
	}
	repo.Write("completion.ps1", generated.Stdout)
	line := "kitsu adviser ask --request 'привіт "
	expected := "'" + file + "'"
	script := ". " + uxPowerShellTestQuote(repo.Path("completion.ps1")) + "; $before = [Console]::OutputEncoding; try { [Console]::OutputEncoding = [Text.Encoding]::ASCII; $line = " + uxPowerShellTestQuote(line) + "; $values = (TabExpansion2 $line $line.Length).CompletionMatches.CompletionText; if ($values -notcontains " + uxPowerShellTestQuote(expected) + ") { throw 'Unicode or quoted completion was corrupted' }; if ([Console]::OutputEncoding.CodePage -ne [Text.Encoding]::ASCII.CodePage) { throw 'Completion changed console encoding' }; 'unicode-ok' } finally { [Console]::OutputEncoding = $before }"
	completed := repo.Run("", "pwsh", "-NoProfile", "-NonInteractive", "-Command", script)
	if completed.Code != 0 || !strings.Contains(completed.Stdout, "unicode-ok") || repo.Exists("completion-executed") {
		t.Fatalf("PowerShell changed or executed literal candidate: %s", completed.Output())
	}
}

func TestUXCompletionFishPreservesQuotedMetacharacters(t *testing.T) {
	if _, err := exec.LookPath("fish"); err != nil {
		t.Skip("fish is not installed")
	}
	repo, _ := skillRepo(t)
	file := "space $(touch completion-executed).json"
	repo.Write(file, "{}")
	generated := repo.Kitsu("completion", "fish")
	if generated.Code != 0 {
		t.Fatal(generated.Output())
	}
	repo.Write("completion.fish", generated.Stdout)
	line := "kitsu adviser ask --request 'space "
	script := "source " + uxShellTestQuote(repo.Path("completion.fish")) + "\nset -l values (complete -C " + uxShellTestQuote(line) + " | string split -f 1 \\t | string unescape)\ncontains -- " + uxShellTestQuote(file) + " $values; or exit 1\nprintf 'literal-ok\\n'\n"
	completed := repo.Run("", "fish", "-c", script)
	if completed.Code != 0 || !strings.Contains(completed.Stdout, "literal-ok") || repo.Exists("completion-executed") {
		t.Fatalf("Fish changed or executed literal candidate: %s", completed.Output())
	}
}

func TestUXCompletionBashPreservesQuotedMetacharacters(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	repo, _ := skillRepo(t)
	file := "space $(touch completion-executed).json"
	repo.Write(file, "{}")
	result := repo.Kitsu("completion", "bash")
	if result.Code != 0 {
		t.Fatal(result.Output())
	}
	repo.Write("completion.bash", result.Stdout)
	line := "kitsu adviser ask --request 'space "
	script := ". " + uxShellTestQuote(repo.Path("completion.bash")) + "\nCOMP_LINE=" + uxShellTestQuote(line) + "\nCOMP_POINT=${#COMP_LINE}\nCOMP_WORDS=(kitsu adviser ask --request 'space ')\nCOMP_CWORD=4\n_kitsu_completion\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
	completed := repo.Run("", "bash", "-c", script)
	if completed.Code != 0 || strings.TrimSpace(completed.Stdout) != file || repo.Exists("completion-executed") {
		t.Fatalf("Bash changed or executed literal candidate: %s", completed.Output())
	}
	wordbreakLine := "kitsu role set --host=co"
	script = ". " + uxShellTestQuote(repo.Path("completion.bash")) + "\nCOMP_LINE=" + uxShellTestQuote(wordbreakLine) + "\nCOMP_POINT=${#COMP_LINE}\nCOMP_WORDS=(kitsu role set --host = co)\nCOMP_CWORD=5\n_kitsu_completion\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
	if completed := repo.Run("", "bash", "-c", script); completed.Code != 0 || strings.TrimSpace(completed.Stdout) != "codex" {
		t.Fatalf("Bash = wordbreak failed: %s", completed.Output())
	}
	writeSkillFixture(t, repo.Path(".agents/skills/demo/SKILL.md"), "demo")
	wordbreakLine = "kitsu skill attach --host codex codex:repo:.agents/skills/de"
	script = ". " + uxShellTestQuote(repo.Path("completion.bash")) + "\nCOMP_LINE=" + uxShellTestQuote(wordbreakLine) + "\nCOMP_POINT=${#COMP_LINE}\nCOMP_WORDS=(kitsu skill attach --host codex codex : repo : .agents/skills/de)\nCOMP_CWORD=9\n_kitsu_completion\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
	if completed := repo.Run("", "bash", "-c", script); completed.Code != 0 || strings.TrimSpace(completed.Stdout) != ".agents/skills/demo" {
		t.Fatalf("Bash : wordbreak failed: %s", completed.Output())
	}
}

func TestUXCompletionScriptsRejectExtraArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"bash", "extra"}, {"unknown"}} {
		var out bytes.Buffer
		if err := runUXCompletion(context.Background(), Config{Out: &out}, args); ExitCode(err) != ExitUsage || out.Len() != 0 {
			t.Fatalf("invalid shell query %q: error %v stdout %q", args, err, out.String())
		}
	}
}

func uxShellTestQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "'\"'\"'") + "'"
}
func uxPowerShellTestQuote(text string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(text), "'", "''") + "'"
}
