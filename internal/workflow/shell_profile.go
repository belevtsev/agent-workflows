package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var shellComments = regexp.MustCompile(`(?m)^[ \t]*#.*$`)
var cwFunctions = regexp.MustCompile(`\b(?:function\s+[^\n{]*\bcw\b|cw\s*\(\s*\)|functions\s*\[\s*["']?cw\b)`)
var shellAliases = regexp.MustCompile(`\balias\s+([^\n;]*)`)
var cwAlias = regexp.MustCompile(`(?:^|\s)["']?cw["']?\s*=`)
var safeShellWord = regexp.MustCompile(`^[a-zA-Z0-9_@%+=:,./-]+$`)

func selectedShell(choice string, paths Paths, environment map[string]string) (string, string) {
	shell := choice
	if shell == "auto" {
		shell = environment["SHELL"]
		if shell == "" {
			shell = "bash"
		}
		shell = filepath.Base(shell)
	}
	if shell == "none" {
		return "", "disabled"
	}
	if shell != "bash" && shell != "zsh" {
		return "", fmt.Sprintf("unsupported shell %s; add cw manually", shell)
	}
	if shell == "zsh" && environment["ZDOTDIR"] != "" {
		location, err := filepath.Abs(environment["ZDOTDIR"])
		if err != nil || location != paths.Home {
			return "", "ZDOTDIR is set; add cw manually or unset ZDOTDIR for home .zshrc enrollment"
		}
	}
	return shell, "enrolled"
}

func shellStartupPath(paths Paths, shell any) (string, error) {
	if shell != "bash" && shell != "zsh" {
		return "", errors.New("Corrupt cw shell selection")
	}
	return filepath.Join(paths.Home, "."+shell.(string)+"rc"), nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if safeShellWord.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func realStartupParents(path string) error {
	parent := filepath.Dir(path)
	var parents []string
	for {
		parents = append(parents, parent)
		ancestor := filepath.Dir(parent)
		if ancestor == parent {
			break
		}
		parent = ancestor
	}
	for i := len(parents) - 1; i >= 0; i-- {
		info, err := os.Lstat(parents[i])
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Unsafe shell startup directory: %s", parents[i])
		}
	}
	return nil
}

func specialFileMode(info os.FileInfo) int {
	mode := int(info.Mode().Perm())
	if info.Mode()&os.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if info.Mode()&os.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if info.Mode()&os.ModeSticky != 0 {
		mode |= 0o1000
	}
	return mode
}

func fileModeBits(mode int) os.FileMode {
	result := os.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		result |= os.ModeSetuid
	}
	if mode&0o2000 != 0 {
		result |= os.ModeSetgid
	}
	if mode&0o1000 != 0 {
		result |= os.ModeSticky
	}
	return result
}

func readStartup(path string) ([]byte, bool, int, error) {
	if err := realStartupParents(path); err != nil {
		return nil, false, 0, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, 0o600, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, fmt.Errorf("Shell startup path is occupied or symlinked: %s", path)
	}
	data, err := os.ReadFile(path)
	return data, true, specialFileMode(info), err
}

func hasCWDefinition(data []byte) bool {
	text := strings.ToValidUTF8(string(data), "\ufffd")
	text = shellComments.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\\\n", "")
	if cwFunctions.MatchString(text) {
		return true
	}
	for _, match := range shellAliases.FindAllStringSubmatch(text, -1) {
		if cwAlias.MatchString(match[1]) {
			return true
		}
	}
	return false
}

func exactFields(value Object, fields ...string) bool {
	if len(value) != len(fields) {
		return false
	}
	for _, field := range fields {
		if _, ok := value[field]; !ok {
			return false
		}
	}
	return true
}

func modeInteger(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, value >= 0 && value <= 0o7777
	case int64:
		return int(value), value >= 0 && value <= 0o7777
	case float64:
		return int(value), value >= 0 && value <= 0o7777 && value == float64(int(value))
	default:
		return 0, false
	}
}
