package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/internal/credentials"
	"github.com/belevtsev/agent-workflows/internal/userconfig"
	"github.com/spf13/cobra"
)

// userHome is the home directory the runtime commands read personal files
// from: --home, else the user's home, made absolute and clean.
func userHome(root *cobra.Command, config Config) (string, error) {
	roots, err := resolveRoots(root, config)
	if err != nil {
		return "", err
	}
	home, err := filepath.Abs(roots.home)
	if err != nil {
		return "", errors.New("resolve home directory")
	}
	return home, nil
}

// credentialDirectory is the private configuration directory that holds the
// saved keys and preferences: --config-dir, else $XDG_CONFIG_HOME/agent-workflows,
// else ~/.config/agent-workflows under the selected home.
func credentialDirectory(root *cobra.Command, config Config) (string, error) {
	home, err := userHome(root, config)
	if err != nil {
		return "", err
	}
	explicit, _ := root.PersistentFlags().GetString("config-dir")
	return userconfig.Dir(home, explicit, config.Getenv)
}

func credentialEnvironment(root *cobra.Command, config Config) (func(string) string, error) {
	directory, err := credentialDirectory(root, config)
	if err != nil {
		return nil, err
	}
	return (credentials.Resolver{Directory: directory, Getenv: config.Getenv}).Environment()
}

func validKey(key string) string {
	if key == "" {
		return ""
	}
	invalid := len(key) > 64<<10 || !utf8.ValidString(key) || strings.TrimSpace(key) != key
	for _, r := range key {
		invalid = invalid || unicode.IsSpace(r) || unicode.IsControl(r)
	}
	if invalid {
		return "A key is one line of text without spaces or control characters."
	}
	return ""
}
