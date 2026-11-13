package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
	"github.com/belevtsev/agent-workflows/v3/internal/userconfig"
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

// credentialEnvironment resolves each adviser key from its environment
// variable, else from the keys saved with cw keys set. Saved keys that cannot
// be read, for example in a configuration directory that is not owner-only,
// are not used: the keys come from the environment alone, one warning goes to
// stderr, and unread says why, so that withheldAdvisers neither reads them
// again nor repeats the warning. Only a configuration directory that cannot
// be located is an error.
func credentialEnvironment(root *cobra.Command, config Config, stderr io.Writer) (getenv func(string) string, unread error, err error) {
	directory, err := credentialDirectory(root, config)
	if err != nil {
		return nil, nil, err
	}
	getenv, err = (credentials.Resolver{Directory: directory, Getenv: config.Getenv}).Environment()
	if err != nil {
		warnUnreadKeys(stderr, err)
		return config.Getenv, err, nil
	}
	return getenv, nil, nil
}

// warnUnreadKeys writes the one warning a command gives when the saved keys
// could not be read.
func warnUnreadKeys(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "warning: saved adviser keys were not read, so none of them is used or opts in an adviser: %v\n", err)
}

