package cli

import (
	"fmt"
	"slices"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
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
// cw keys set. Without --policy it reads the saved keys, so an unreadable
// credential store is an error, as is an unreadable preferences file.
func withheldAdvisers(root *cobra.Command, config Config, explicitPolicy bool) ([]string, error) {
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
	presence, err := (credentials.Resolver{Directory: directory, Getenv: config.Getenv}).Presence()
	if err != nil {
		return nil, err
	}
	saved := func(variable string) bool { return presence[variable].Stored }
	var withheld []string
	for _, provider := range modelpolicy.Providers() {
		if slices.Contains(preferences.Disabled, provider) || !optedIn(provider, preferences, saved) {
			withheld = append(withheld, provider)
		}
	}
	return withheld, nil
}

// keyWithoutOptInWarnings returns one doctor warning, in provider order, for
// each adviser whose key variable is set in the environment although the user
// has neither opted in to it (advisers.enabled or a key saved with
// cw keys set) nor disabled it. Such a key binds no adviser without --policy.
// An unreadable preferences file yields one warning instead.
func keyWithoutOptInWarnings(directory string, report projectctx.DoctorReport) []string {
	preferences, err := adviserPreferences(directory)
	if err != nil {
		return []string{"adviser opt-in was not checked: " + err.Error()}
	}
	saved := func(variable string) bool { return report.Credentials.Stored[variable] }
	var warnings []string
	for _, provider := range modelpolicy.Providers() {
		variable, _ := consult.CredentialVariable(provider)
		if !report.Credentials.Environment[variable] || slices.Contains(preferences.Disabled, provider) || optedIn(provider, preferences, saved) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("%s: key present, adviser not enabled; %s alone does not bind it (opt in with advisers.enabled, cw keys set or --policy)", provider, variable))
	}
	return warnings
}
