package workflow

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateAutomation keeps repository tooling in Go. Scripts belonging to skills
// are resources, with their own runtime requirements, rather than manager code.
func ValidateAutomation(root string) error {
	root, err := suiteRoot(root, true)
	if err != nil {
		return err
	}
	files, err := suiteInspectTree(root)
	if err != nil {
		return err
	}
	for _, file := range files {
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		extension := strings.ToLower(filepath.Ext(name))
		if strings.HasPrefix(name, "skills/") && extension == ".py" {
			return fmt.Errorf("first-party skills must use Go: %s", name)
		}
		if name == "install.sh" || strings.HasPrefix(name, "skills/") || strings.HasPrefix(name, "third_party/") {
			continue
		}
		base := strings.ToLower(filepath.Base(name))
		// An eval case's scaffold is a fixture for `claude plugin eval`, which
		// runs only a Bash script to seed a case's workspace. It is a resource
		// of the eval suite, like a skill's scripts, not repository tooling.
		if base == "scaffold.sh" && strings.HasPrefix(name, "evals/") {
			continue
		}
		if extension == ".py" || extension == ".sh" || extension == ".bash" || extension == ".zsh" ||
			(strings.HasPrefix(base, "requirements") && extension == ".txt") {
			return fmt.Errorf("first-party automation must use Go: %s", name)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, []byte("#!")) {
			continue
		}
		line, _, _ := bytes.Cut(data, []byte("\n"))
		for word := range strings.FieldsSeq(string(line)) {
			interpreter := strings.TrimPrefix(filepath.Base(word), "#!")
			if strings.HasPrefix(interpreter, "python") || interpreter == "sh" || interpreter == "bash" || interpreter == "zsh" {
				return fmt.Errorf("first-party script interpreter must use Go: %s", name)
			}
		}
	}
	return nil
}
