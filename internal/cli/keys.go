package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
	"github.com/spf13/cobra"
)

// keysReport is the result of cw keys set or unset. It never carries a key.
type keysReport struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	// Status is saved, removed or unchanged, or persistence_unconfirmed when
	// the change was committed but its durability could not be verified.
	Status             string `json:"status"`
	Committed          bool   `json:"committed"`
	DurabilityVerified bool   `json:"durability_verified"`
}

// keysStatus is the result of cw keys status: where each key comes from,
// without any value.
type keysStatus struct {
	Directory string                          `json:"directory"`
	Keys      map[string]credentials.Presence `json:"keys"`
}

// keyName accepts exactly one of the three adviser credential names.
func keyName(_ *cobra.Command, args []string) error {
	if len(args) != 1 || !slices.Contains(credentials.Names, args[0]) {
		return usage(fmt.Errorf("NAME must be one of %s", strings.Join(credentials.Names, ", ")))
	}
	return nil
}

// maxKeyInput bounds what readKey accepts: a key of the store's 64 KiB limit
// plus one CRLF. Anything longer is refused rather than truncated.
const maxKeyInput = 64<<10 + 2

// readKey reads one key from standard input: the whole input, minus one
// trailing line break. The store's limits apply to its length and content.
func readKey(in io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(in, maxKeyInput+1))
	if err != nil {
		return "", errors.New("cannot read the key from standard input")
	}
	if len(data) > maxKeyInput {
		return "", errors.New("A key is one line of text without spaces or control characters, at most 64 KiB.")
	}
	key := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if key == "" {
		return "", errors.New("no key was read from standard input")
	}
	if message := validKey(key); message != "" {
		return "", errors.New(message)
	}
	return key, nil
}

func addKeysCommand(root *cobra.Command, config Config) {
	keys := &cobra.Command{
		Use:   "keys",
		Short: "Save, remove or report the adviser keys in the private configuration directory",
		Long: "Manage the saved adviser keys (TYPESAFE_API_KEY, SUBQ_API_KEY and OPENAI_API_KEY) in the configuration directory: --config-dir, else $XDG_CONFIG_HOME/agent-workflows, else ~/.config/agent-workflows. " +
			"A key set in the environment takes precedence over a saved one. Saved keys live in a private plaintext file.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	guard := func(cmd *cobra.Command) error {
		if dryRun, _ := root.PersistentFlags().GetBool("dry-run"); dryRun {
			return usage(fmt.Errorf("keys %s rejects --dry-run; no key was changed", cmd.Name()))
		}
		return nil
	}
	save := func(cmd *cobra.Command, name string, value *string) error {
		directory, err := credentialDirectory(root, config)
		if err != nil {
			return err
		}
		report := keysReport{Name: name, Action: cmd.Name(), Status: "unchanged"}
		result, err := (credentials.Store{Directory: directory}).Save(cmd.Context(), map[string]*string{name: value}, nil)
		report.Committed, report.DurabilityVerified = result.Committed, result.DurabilityVerified
		switch {
		case err != nil && result.Committed:
			report.Status = "persistence_unconfirmed"
		case err != nil:
			return err
		case result.Committed && value == nil:
			report.Status = "removed"
		case result.Committed:
			report.Status = "saved"
		}
		if writeErr := writeJSON(cmd.OutOrStdout(), report); writeErr != nil {
			return writeErr
		}
		return err
	}
	var fromEnv string
	set := &cobra.Command{
		Use:   "set NAME",
		Short: "Save one adviser key read from standard input, or from the variable named by --from-env",
		Long: "Save the key for NAME (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY). The key is read from standard input, minus one trailing line break, or with --from-env VAR from that environment variable; it is never taken from the command line. " +
			"A key is one line of text without spaces or control characters, at most 64 KiB.",
		Args: keyName,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := guard(cmd); err != nil {
				return err
			}
			var value string
			if fromEnv != "" {
				value = config.Getenv(fromEnv)
				if value == "" {
					return fmt.Errorf("environment variable %s is not set", fromEnv)
				}
				if message := validKey(value); message != "" {
					return errors.New(message)
				}
			} else {
				var err error
				if value, err = readKey(cmd.InOrStdin()); err != nil {
					return err
				}
			}
			return save(cmd, args[0], &value)
		},
	}
	set.Flags().StringVar(&fromEnv, "from-env", "", "Read the key from this environment variable instead of standard input")
	keys.AddCommand(set)
	keys.AddCommand(&cobra.Command{
		Use:   "unset NAME",
		Short: "Remove one saved adviser key",
		Args:  keyName,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := guard(cmd); err != nil {
				return err
			}
			return save(cmd, args[0], nil)
		},
	})
	keys.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Report whether each adviser key comes from the environment, the saved keys or neither",
		Args:  noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			directory, err := credentialDirectory(root, config)
			if err != nil {
				return err
			}
			presence, err := (credentials.Resolver{Directory: directory, Getenv: config.Getenv}).Presence()
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), keysStatus{Directory: directory, Keys: presence})
		},
	})
	root.AddCommand(keys)
}
