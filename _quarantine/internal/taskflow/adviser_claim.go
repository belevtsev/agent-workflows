package taskflow

import (
	"maps"
	"slices"
	"strings"
)

// claimsNoAdvisers reports whether a plan's checkpoint reason claims that no
// advisers are configured, bound or available, or that no adviser credentials
// are present, under the phrase rules of the interface contract.
func claimsNoAdvisers(reason string) bool {
	words := strings.Fields(normaliseClaim(reason))
	for i := range words {
		for _, rule := range noAdviserRules {
			if matchClaim(words, i, rule) {
				return true
			}
		}
	}
	return false
}

// adviserClaimConflict returns the shared message prefix when the plan's
// not-required checkpoint reason claims no advisers while the binding names
// some, and false otherwise.
func adviserClaimConflict(plan Plan, advisers map[string]string) (string, bool) {
	if plan.Checkpoints == nil || plan.Checkpoints.Required || len(advisers) == 0 || !claimsNoAdvisers(plan.Checkpoints.Reason) {
		return "", false
	}
	return "plan checkpoints reason claims no advisers are configured, but this task binds " + strings.Join(slices.Sorted(maps.Keys(advisers)), ", "), true
}

// normaliseClaim lowercases ASCII A-Z only and turns every byte that is not
// a-z or 0-9 into a space, so invalid UTF-8 and non-ASCII letters separate
// words instead of being case-mapped.
func normaliseClaim(text string) string {
	out := []byte(text)
	for i, b := range out {
		switch {
		case b >= 'A' && b <= 'Z':
			out[i] = b + ('a' - 'A')
		case b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		default:
			out[i] = ' '
		}
	}
	return string(out)
}

// claimStep is one position of a phrase rule: one of its alternatives (each
// one or more words), or any all-digit word, possibly skipped when optional.
type claimStep struct {
	alts     [][]string
	digits   bool
	optional bool
}

func claimWords(phrases ...string) claimStep {
	step := claimStep{}
	for _, phrase := range phrases {
		step.alts = append(step.alts, strings.Fields(phrase))
	}
	return step
}

func claimJoin(steps ...claimStep) claimStep {
	step := claimStep{}
	for _, s := range steps {
		step.alts = append(step.alts, s.alts...)
	}
	return step
}

func claimOptional(step claimStep) claimStep {
	step.optional = true
	return step
}

var (
	claimADV    = claimWords("adviser", "advisers", "advisor", "advisors")
	claimSRC    = claimJoin(claimADV, claimWords("provider", "providers"))
	claimBE     = claimWords("are", "is", "were", "was", "have been", "has been")
	claimHAVE   = claimWords("has", "have")
	claimSTATE  = claimWords("configured", "bound", "available", "enabled", "present", "set up", "set")
	claimPRE    = claimWords("configured", "bound", "available", "enabled")
	claimNEG    = claimWords("not", "never")
	claimCONTR  = claimWords("aren t", "isn t", "weren t", "wasn t", "haven t been", "hasn t been")
	claimCRED   = claimWords("credential", "credentials")
	claimKEY    = claimWords("key", "keys")
	claimDIGITS = claimStep{digits: true}
	claimNo     = claimWords("no")
	claimZero   = claimWords("0", "zero")
)

// noAdviserRules are the phrases C1-C10 of the interface contract.
var noAdviserRules = [][]claimStep{
	{claimNo, claimPRE, claimADV},                                                              // C1
	{claimNo, claimSRC, claimOptional(claimBE), claimSTATE},                                    // C2
	{claimADV, claimOptional(claimBE), claimNEG, claimSTATE},                                   // C3
	{claimADV, claimCONTR, claimSTATE},                                                         // C3
	{claimADV, claimHAVE, claimNEG, claimWords("been"), claimSTATE},                            // C3
	{claimNo, claimSRC, claimOptional(claimHAVE), claimOptional(claimWords("api")), claimCRED}, // C4
	{claimNo, claimSRC, claimOptional(claimHAVE), claimOptional(claimWords("api")), claimKEY},  // C5
	{claimZero, claimWords("of"), claimDIGITS, claimOptional(claimSRC), claimCRED},             // C6
	{claimZero, claimADV}, // C7
	{claimWords("none of the"), claimADV, claimOptional(claimBE), claimSTATE}, // C8
	{claimWords("without"), claimADV, claimCRED},                              // C9
	{claimADV, claimCRED, claimOptional(claimBE), claimWords("missing")},      // C10
}

// matchClaim reports whether rule matches the words starting at index i,
// trying every alternative and optional skip; rules are short, so the search
// is bounded by a small constant per start index.
func matchClaim(words []string, i int, rule []claimStep) bool {
	if len(rule) == 0 {
		return true
	}
	step := rule[0]
	if step.optional && matchClaim(words, i, rule[1:]) {
		return true
	}
	if step.digits {
		return i < len(words) && allDigits(words[i]) && matchClaim(words, i+1, rule[1:])
	}
	for _, alt := range step.alts {
		if i+len(alt) <= len(words) && slices.Equal(words[i:i+len(alt)], alt) && matchClaim(words, i+len(alt), rule[1:]) {
			return true
		}
	}
	return false
}

func allDigits(word string) bool {
	for i := 0; i < len(word); i++ {
		if word[i] < '0' || word[i] > '9' {
			return false
		}
	}
	return word != ""
}
