package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
	"github.com/belevtsev/agent-workflows/v3/internal/doctor"
	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
	"github.com/belevtsev/agent-workflows/v3/internal/userconfig"
	"github.com/spf13/cobra"
)

// adviserPreferences reads advisers.enabled and advisers.disabled from the
// saved preferences in the configuration directory. A missing preferences
// file enables and disables none; an unreadable or invalid one is an error.
func adviserPreferences(directory string) (userconfig.Advisers, error) {
	advisers, err := userconfig.AdviserPreferences(directory)
	if err != nil {
		return userconfig.Advisers{}, fmt.Errorf("cannot read saved preferences: %w", err)
	}
	return advisers, nil
}

// optedIn reports whether the user opted in to provider without --policy:
// advisers.enabled lists it, or its key was saved with cw keys set. A key in
// the environment alone is not an opt-in.
func optedIn(provider string, preferences userconfig.Advisers, saved func(variable string) bool) bool {
	variable, _ := consult.CredentialVariable(provider)
	return slices.Contains(preferences.Enabled, provider) || saved(variable)
}

// withheldAdvisers returns, in provider order, the advisers that are neither
// bound to a new task nor contacted by an ad hoc consult: those the saved
// preferences disable and, unless the command was given --policy, those the
// user has not opted in to with advisers.enabled or a key saved with
// cw keys set. An unreadable preferences file is an error. Saved keys that
// cannot be read opt in to nothing: the command goes on and says so on
// stderr, so an unsafe key store never adds an adviser. unread is
// credentialEnvironment's reason for not reading them, which it has already
// reported; nil means they were read or were not needed.
func withheldAdvisers(root *cobra.Command, config Config, explicitPolicy bool, unread error, stderr io.Writer) ([]string, error) {
	directory, err := credentialDirectory(root, config)
	if err != nil {
		return nil, err
	}
	preferences, err := adviserPreferences(directory)
	if err != nil {
		return nil, err
	}
	if explicitPolicy {
		return preferences.Disabled, nil
	}
	saved := func(string) bool { return false }
	if unread == nil {
		presence, err := (credentials.Resolver{Directory: directory, Getenv: config.Getenv}).Presence()
		if err != nil {
			warnUnreadKeys(stderr, err)
		} else {
			saved = func(variable string) bool { return presence[variable].Stored }
		}
	}
	var withheld []string
	for _, provider := range modelpolicy.Providers() {
		if slices.Contains(preferences.Disabled, provider) || !optedIn(provider, preferences, saved) {
			withheld = append(withheld, provider)
		}
	}
	return withheld, nil
}

// keyWithoutOptInNotice is the part of a doctor warning that marks a key set
// without an opt-in. The warning is information, so it never raises the
// summary status to attention.
const keyWithoutOptInNotice = ": key present, adviser not enabled; "

// isKeyWithoutOptInWarning reports whether warning is one that
// keyWithoutOptInWarnings writes.
func isKeyWithoutOptInWarning(warning string) bool {
	for _, provider := range modelpolicy.Providers() {
		if strings.HasPrefix(warning, provider+keyWithoutOptInNotice) {
			return true
		}
	}
	return false
}

// keyWithoutOptInWarnings returns one doctor warning, in provider order, for
// each adviser that the policy selected without --policy enables and whose key
// variable is set in the environment although the user has neither opted in
// to it (advisers.enabled or a key saved with cw keys set) nor disabled it.
// When the saved keys could not be read it returns none, since any of them
// may be saved; the credentials_stored check reports the store. An unusable
// policy or preferences file yields one warning instead.
func keyWithoutOptInWarnings(root *cobra.Command, config Config, directory string, report doctor.DoctorReport) []string {
	source, err := selectPolicy(root, config, "")
	if err != nil {
		return []string{"adviser opt-in was not checked: " + err.Error()}
	}
	selected, err := source.Load()
	if err != nil {
		return []string{"adviser opt-in was not checked: " + err.Error()}
	}
	preferences, err := adviserPreferences(directory)
	if err != nil {
		return []string{"adviser opt-in was not checked: " + err.Error()}
	}
	if report.Credentials.Stored == nil {
		return nil
	}
	saved := func(variable string) bool { return report.Credentials.Stored[variable] }
	var warnings []string
	for _, provider := range modelpolicy.Providers() {
		variable, _ := consult.CredentialVariable(provider)
		if _, enabled := selected.Providers[provider]; !enabled || !report.Credentials.Environment[variable] ||
			slices.Contains(preferences.Disabled, provider) || optedIn(provider, preferences, saved) {
			continue
		}
		warnings = append(warnings, provider+keyWithoutOptInNotice+variable+" alone does not bind it (opt in with advisers.enabled or cw keys set)")
	}
	return warnings
}
