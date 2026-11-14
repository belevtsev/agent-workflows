package taskflow

// Contract: verify-adviser-claim, interface contract revision 4
// (contract-r4.json sha256 75db8e224d9ed6b355853f694ca8eff1fb3829b8c395e9770af3174a93bfc428).
// Requirements R1 (claimsNoAdvisers), R2 (Start refusal), R4 (Verify problem)
// and R5 (unchanged behaviour). Every expected value below is taken from the
// contract text; acReferenceClaims is an independent transcription of the R1
// rules used as a property oracle.

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// ---- shared contract literals ----

// acClaimPrefix is the common prefix of every R2 and R4 message.
const acClaimPrefix = "plan checkpoints reason claims no advisers are configured, but this task binds "

// acStartMessage is the exact R2 refusal for the given bound IDs.
func acStartMessage(ids string) string {
	return acClaimPrefix + ids + `; set "required": true and run the checkpoints, or, if checkpoints are truly unneeded, give the real reason`
}

// acVerifyAttachFinal is the R4 message with a usable before checkpoint.
func acVerifyAttachFinal(ids string) string {
	return acClaimPrefix + ids + "; attach the final checkpoint"
}

// acVerifyUnchanged is the R4 message with no usable before checkpoint and
// the source unchanged since start.
func acVerifyUnchanged(ids string) string {
	return acClaimPrefix + ids + "; attach the before and final checkpoints, or ask the user before abandoning this record and starting a corrected one"
}

// acVerifyChanged is the R4 message with no usable before checkpoint and the
// source changed since start.
func acVerifyChanged(ids string) string {
	return acClaimPrefix + ids + "; the source changed since start, so the before checkpoint can no longer be attached and a new record must not start over the changed source: stop and ask the user how to proceed"
}

// acClaimReason is the first R1 required-true example (C2, C6), the reason
// real tasks used while binding advisers.
const acClaimReason = "No advisers are configured on this host (0 of 3 credentials)."

// acBenignReason is an R1 required-false example.
const acBenignReason = "Single-file documentation fix; adviser checkpoints add no decision value here."

// acOwnerReason is the R5 example of a reason for which claimsNoAdvisers is false.
const acOwnerReason = "the owner fixed the design"

// acDefaultIDs are the bound IDs of fixtureBinding (subq_snap, openai_decisions)
// sorted ascending and joined with ", ", as the R2 example states.
const acDefaultIDs = "openai_decisions, subq_snap"

func acCount(lines []string, want string) int {
	count := 0
	for _, line := range lines {
		if line == want {
			count++
		}
	}
	return count
}

func acCountContaining(lines []string, fragment string) int {
	count := 0
	for _, line := range lines {
		if strings.Contains(line, fragment) {
			count++
		}
	}
	return count
}

// ---- R1: claimsNoAdvisers ----

// acClaims calls claimsNoAdvisers and turns a panic into a test failure: R1
// says the function never panics, for any input.
func acClaims(t *testing.T, reason string) (result bool) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("R1 violated: claimsNoAdvisers panicked for %q: %v", acShort(reason), recovered)
			result = false
		}
	}()
	return claimsNoAdvisers(reason)
}

// acClaimsQuiet is acClaims without the test context, for property loops that
// report their own failures; ok is false when the call panicked.
func acClaimsQuiet(reason string) (result, ok bool) {
	defer func() {
		if recover() != nil {
			result, ok = false, false
		}
	}()
	return claimsNoAdvisers(reason), true
}

func acShort(s string) string {
	if len(s) > 120 {
		return s[:120] + fmt.Sprintf("...(%d bytes)", len(s))
	}
	return s
}

// acTrueExamples are the R1 "Required true examples", the known false
// positives (true by the rules) and further inputs the rules make true.
var acTrueExamples = []struct{ input, phrase string }{
	// Required true examples, verbatim.
	{"No advisers are configured on this host (0 of 3 credentials).", "C2, C6"},
	{"The owner's brief fixes the command's inputs; no adviser credentials are configured and no alternative approach is open", "C4"},
	{"no cross-platform code changes; no adviser has credentials", "C4 (HAVE)"},
	{"the approach is fixed and no adviser is configured", "C2"},
	{"no adviser credential is configured (cw doctor: 0 of 3)", "C4"},
	{"no_advisers_configured", "C2"},
	{"No advisers configured.", "C2"},
	{"No configured advisers on this host", "C1"},
	{"NO ADVISORS AVAILABLE", "C2"},
	{"No providers are configured", "C2"},
	{"Advisers are not configured here", "C3"},
	{"advisers aren't configured", "C3"},
	{"advisers have not been configured", "C3"},
	{"the adviser was never bound", "C3"},
	{"no adviser set up", "C2"},
	{"no adviser has been set up", "C2 (BE = has been, STATE = set up)"},
	{"No adviser credentials.", "C4"},
	{"no provider API keys", "C5"},
	{"0 of 3 provider credentials present", "C6"},
	{"zero advisers bound", "C7"},
	{"none of the advisers are configured", "C8"},
	{"proceeding without adviser credentials", "C9"},
	{"adviser credentials are missing", "C10"},
	{"Design fixed by the owner. no-advisers-bound", "C2"},
	// Known false positives: the rules return true.
	{"Docs only; the change touches no provider credentials.", "C4 (known false positive)"},
	{"Tests cover the case where no adviser is bound", "C2 (known false positive)"},
	// Further rule consequences.
	{"advisers aren’t configured", "C3 (U+2019 apostrophe is a separator)"},
	{"advisers haven't been set up", "C3 CONTR haven t been, STATE set up"},
	{"the adviser hasn't been enabled", "C3 CONTR hasn t been"},
	{"advisers have never been bound", "C3 HAVE NEG been"},
	{"advisors were not present", "C3 BE were"},
	{"0 of 12 adviser credentials", "C6 multi-digit DIGITS with SRC"},
	{"0 of 0 provider credentials", "C6 DIGITS may be 0"},
	{"zero of 3 credentials", "C6 zero, SRC omitted"},
	{"0 advisors", "C7"},
	{"none of the advisors available", "C8 BE omitted"},
	{"without advisor credential", "C9"},
	{"advisers credential was missing", "C10 BE was"},
	{"no providers have api keys", "C5 HAVE and api"},
	{"no provider key", "C5"},
	{"no adviser has api credentials", "C4 HAVE and api"},
	{"no available advisors", "C1 PRE available"},
	{"no enabled adviser", "C1 PRE enabled"},
	{"no providers were set", "C2 STATE set"},
	{"no advisers—configured", "C2 non-ASCII separators"},
	{"no advisers \xff configured", "C2 invalid UTF-8 byte is a separator"},
	{"no\xffadvisers\xfe\xfdconfigured", "C2 runs of invalid bytes are one separator"},
	{"no \t\n advisers --- configured!!!", "C2 separator runs collapse"},
	{"No AdViSeRs CoNfIgUrEd", "C2 mixed ASCII case"},
	{"no provider Keys", "C5 ASCII K lowercases"},
	{"advisers not configured", "C3 with BE omitted"},
}

// acFalseExamples are the R1 "Required false examples", the known false
// negatives (false by the rules) and further inputs the rules make false.
var acFalseExamples = []struct{ input, why string }{
	// Required false examples, verbatim.
	{"", "empty: no phrase"},
	{"Single-file documentation fix; adviser checkpoints add no decision value here.", "adviser checkpoints fits no phrase"},
	{"The documented contract fixes the result; no alternative approach is open", "no SRC, CRED or KEY phrase"},
	{"The owner's brief fixes the design; advisers openai_decisions and subq_snap are bound", "no absence word tied to SRC"},
	{"no adviser checkpoint is needed: the design is fixed", "no ADV is followed by checkpoint"},
	{"proceed without adviser checkpoints", "C9 needs CRED"},
	{"Handles 0 providers and 2 providers alike; design fixed by the owner.", "C7 needs ADV"},
	{"The change runs without provider API keys, so tests need no network.", "C9 needs ADV CRED"},
	{"Adds a clear error when provider keys are missing; the approach is fixed.", "C10 needs ADV CRED"},
	{"advisers are configured and bound", "no negation"},
	{"noadvisers configured", "noadvisers is one word"},
	{"known advisers configured", "no must be a whole word"},
	{"no credentials", "C4 needs SRC after no"},
	{"no api key", "C5 needs SRC after no"},
	{"Docs-only change; no credentials, keys or secrets are touched.", "no SRC"},
	{"The fix reads no API keys and changes no schema.", "no SRC after no"},
	{"Pure refactor of the credential loader; no credential handling changes.", "no SRC"},
	{"no keys changed", "no SRC"},
	// Known false negatives: the rules return false.
	{"there are no advisers", "known false negative"},
	{"Advisers unavailable", "known false negative"},
	{"no one configured the advisers", "known false negative"},
	// Normalisation: only A-Z are case-mapped, everything else not a-z/0-9 is a separator.
	{"no provider Keys", "U+212A KELVIN SIGN is not case-mapped, so the word is eys"},
	{"no adviser KEY", "U+212A KELVIN SIGN is not case-mapped"},
	{"no advİsers configured", "U+0130 is not case-mapped, so advisers is split"},
	{"ＮＯ ＡＤＶＩＳＥＲＳ ＣＯＮＦＩＧＵＲＥＤ", "full-width letters are separators"},
	{"no advi\xffsers configured", "an invalid byte inside a word splits it"},
	// Whole-word boundaries.
	{"xno advisers configured", "no must start a word"},
	{"no advisers configuredx", "STATE must end a word"},
	{"no advisersx configured", "SRC must end a word"},
	{"no advisers setup", "set up is two words"},
	{"0of3 credentials", "0of3 is one word"},
	{"00 advisers", "C7 needs the word 0, not 00"},
	{"10 advisers", "C7 needs the word 0"},
	{"00 of 3 credentials", "C6 needs the word 0"},
	{"0 of three credentials", "C6 needs DIGITS"},
	{"0 of credentials", "C6 needs DIGITS"},
	{"no advisers are configuredly", "configuredly is not STATE"},
	{"advisers are configured", "C3 needs NEG"},
	{"advisers configured not", "C3 needs NEG before STATE"},
	{"none of advisers are configured", "C8 needs none of the"},
	{"advisers missing credentials", "C10 needs ADV CRED"},
	{"without credentials", "C9 needs ADV"},
	{"zero providers", "C7 needs ADV"},
	{"no providers checked", "checked is not STATE"},
}

// R1: every contract example returns its stated value. Fails when a phrase
// rule is missing, misread or too broad.
func TestClaimsNoAdvisersExamples(t *testing.T) {
	for _, example := range acTrueExamples {
		if !acClaims(t, example.input) {
			t.Errorf("R1: claimsNoAdvisers(%q) = false, want true (%s)", example.input, example.phrase)
		}
	}
	for _, example := range acFalseExamples {
		if acClaims(t, example.input) {
			t.Errorf("R1: claimsNoAdvisers(%q) = true, want false (%s)", example.input, example.why)
		}
	}
}

// R1 word classes, transcribed from the contract.
var (
	acADV   = []string{"adviser", "advisers", "advisor", "advisors"}
	acSRC   = append(slices.Clone(acADV), "provider", "providers")
	acBE    = []string{"are", "is", "were", "was", "have been", "has been"}
	acHAVE  = []string{"has", "have"}
	acSTATE = []string{"configured", "bound", "available", "enabled", "present", "set up", "set"}
	acPRE   = []string{"configured", "bound", "available", "enabled"}
	acNEG   = []string{"not", "never"}
	acCONTR = []string{"aren t", "isn t", "weren t", "wasn t", "haven t been", "hasn t been"}
	acCRED  = []string{"credential", "credentials"}
	acKEY   = []string{"key", "keys"}
	// DIGITS samples exclude the word 0: "0 of 0 adviser credentials" also
	// contains C7 ("0 adviser"), which would defeat the glued-word property.
	// The word 0 as DIGITS is covered by an explicit example.
	acDIGITS = []string{"3", "12", "007", "1234567890"}
)

// acOptional returns the class with the empty alternative added.
func acOptional(class []string) []string { return append([]string{""}, class...) }

// acJoin joins non-empty words with single spaces.
func acJoin(words ...string) string {
	kept := words[:0:0]
	for _, word := range words {
		if word != "" {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

// acCross returns every phrase of the sequence of classes.
func acCross(classes ...[]string) []string {
	phrases := []string{""}
	for _, class := range classes {
		var next []string
		for _, prefix := range phrases {
			for _, word := range class {
				next = append(next, acJoin(prefix, word))
			}
		}
		phrases = next
	}
	return phrases
}

func acWord(word string) []string { return []string{word} }

// acPhrases enumerates every normalised phrase of rules C1 to C10.
func acPhrases() map[string][]string {
	zero := []string{"0", "zero"}
	return map[string][]string{
		"C1": acCross(acWord("no"), acPRE, acADV),
		"C2": acCross(acWord("no"), acSRC, acOptional(acBE), acSTATE),
		"C3": append(append(
			acCross(acADV, acOptional(acBE), acNEG, acSTATE),
			acCross(acADV, acCONTR, acSTATE)...),
			acCross(acADV, acHAVE, acNEG, acWord("been"), acSTATE)...),
		"C4":  acCross(acWord("no"), acSRC, acOptional(acHAVE), acOptional(acWord("api")), acCRED),
		"C5":  acCross(acWord("no"), acSRC, acOptional(acHAVE), acOptional(acWord("api")), acKEY),
		"C6":  acCross(zero, acWord("of"), acDIGITS, acOptional(acSRC), acCRED),
		"C7":  acCross(zero, acADV),
		"C8":  acCross(acWord("none of the"), acADV, acOptional(acBE), acSTATE),
		"C9":  acCross(acWord("without"), acADV, acCRED),
		"C10": acCross(acADV, acCRED, acOptional(acBE), acWord("missing")),
	}
}

// R1 property: every phrase the grammar defines is detected alone and when
// embedded in other text between separators. Fails when any alternative of
// any word class or any optional element is not handled.
func TestClaimsNoAdvisersEveryGrammarPhrase(t *testing.T) {
	total := 0
	for rule, phrases := range acPhrases() {
		for _, phrase := range phrases {
			total++
			for _, input := range []string{
				phrase,
				"Reason: " + phrase + ".",
				"(" + phrase + ")",
				"design fixed by the owner; " + phrase + "; nothing else",
			} {
				got, ok := acClaimsQuiet(input)
				if !ok {
					t.Fatalf("R1 violated: claimsNoAdvisers panicked for %q", input)
				}
				if !got {
					t.Errorf("R1 %s: claimsNoAdvisers(%q) = false, want true", rule, input)
				}
			}
		}
	}
	if total < 1000 {
		t.Fatalf("grammar enumeration produced only %d phrases", total)
	}
}

// R1 property (whole words): gluing any two adjacent words of a phrase into
// one word breaks the match. Fails when matching is on substrings rather than
// whole words of the normalised text.
func TestClaimsNoAdvisersGluedWordsDoNotMatch(t *testing.T) {
	for rule, phrases := range acPhrases() {
		for _, phrase := range phrases {
			words := strings.Split(phrase, " ")
			for index := 0; index+1 < len(words); index++ {
				glued := acJoin(strings.Join(words[:index], " "), words[index]+words[index+1], strings.Join(words[index+2:], " "))
				got, ok := acClaimsQuiet(glued)
				if !ok {
					t.Fatalf("R1 violated: claimsNoAdvisers panicked for %q", glued)
				}
				if got {
					t.Errorf("R1 %s: claimsNoAdvisers(%q) = true for glued words of %q, want false", rule, glued, phrase)
				}
			}
		}
	}
}

// acNormalise transcribes the R1 normalisation: lowercase A-Z only, then
// replace every maximal run of bytes outside a-z and 0-9 with one space.
// Working on bytes is equivalent to runes here: every byte of a multi-byte or
// invalid UTF-8 sequence is outside a-z and 0-9.
func acNormalise(s string) string {
	var builder strings.Builder
	separator := false
	for index := 0; index < len(s); index++ {
		c := s[index]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			builder.WriteByte(c)
			separator = false
		} else if !separator {
			builder.WriteByte(' ')
			separator = true
		}
	}
	return builder.String()
}

func acAlt(class []string) string { return "(?:" + strings.Join(class, "|") + ")" }

// acReferencePattern transcribes rules C1 to C10 over the normalised text,
// each bounded by the start or end of the text or a space.
var acReferencePattern = func() *regexp.Regexp {
	adv, src, be, have := acAlt(acADV), acAlt(acSRC), acAlt(acBE), acAlt(acHAVE)
	state, pre, neg, contr := acAlt(acSTATE), acAlt(acPRE), acAlt(acNEG), acAlt(acCONTR)
	cred, key, digits := acAlt(acCRED), acAlt(acKEY), "[0-9]+"
	rules := []string{
		"no " + pre + " " + adv,
		"no " + src + "(?: " + be + ")? " + state,
		adv + "(?: " + be + ")? " + neg + " " + state,
		adv + " " + contr + " " + state,
		adv + " " + have + " " + neg + " been " + state,
		"no " + src + "(?: " + have + ")?(?: api)? " + cred,
		"no " + src + "(?: " + have + ")?(?: api)? " + key,
		"(?:0|zero) of " + digits + "(?: " + src + ")? " + cred,
		"(?:0|zero) " + adv,
		"none of the " + adv + "(?: " + be + ")? " + state,
		"without " + adv + " " + cred,
		adv + " " + cred + "(?: " + be + ")? missing",
	}
	return regexp.MustCompile("(?:^| )(?:" + strings.Join(rules, "|") + ")(?: |$)")
}()

// acReferenceClaims is the independent R1 oracle for property tests.
func acReferenceClaims(reason string) bool {
	return acReferencePattern.MatchString(acNormalise(reason))
}

// Fixture invariant: the reference transcription agrees with every contract
// example, so it is a valid oracle for the random-input property below.
func TestClaimsNoAdvisersReferenceMatchesContractExamples(t *testing.T) {
	t.Parallel()
	for _, example := range acTrueExamples {
		if !acReferenceClaims(example.input) {
			t.Errorf("fixture invariant: reference rejects true example %q", example.input)
		}
	}
	for _, example := range acFalseExamples {
		if acReferenceClaims(example.input) {
			t.Errorf("fixture invariant: reference accepts false example %q", example.input)
		}
	}
	// The grammar enumeration and the glued-word variants used by the
	// property tests agree with the reference too.
	for rule, phrases := range acPhrases() {
		for _, phrase := range phrases {
			if !acReferenceClaims(phrase) {
				t.Errorf("fixture invariant: reference rejects %s phrase %q", rule, phrase)
			}
			words := strings.Split(phrase, " ")
			for index := 0; index+1 < len(words); index++ {
				glued := acJoin(strings.Join(words[:index], " "), words[index]+words[index+1], strings.Join(words[index+2:], " "))
				if acReferenceClaims(glued) {
					t.Errorf("fixture invariant: reference accepts glued %q", glued)
				}
			}
		}
	}
	random := rand.New(rand.NewPCG(20261108, 4))
	phrases, positives := acAllPhrases(), 0
	for trial := 0; trial < 20000; trial++ {
		if acReferenceClaims(acRandomReason(random, phrases)) {
			positives++
		}
	}
	if positives < 1000 || positives > 19000 {
		t.Errorf("fixture invariant: generator produced %d positive reasons of 20000", positives)
	}
}

// acVocabulary mixes class words, near misses and noise.
var acVocabulary = func() []string {
	words := []string{"no", "none", "of", "the", "without", "api", "missing", "been", "zero", "known", "noadvisers",
		"checkpoint", "checkpoints", "design", "owner", "fixed", "host", "one", "there", "unavailable", "setup",
		"t", "aren", "isn", "haven", "hasn", "weren", "wasn", "0", "00", "3", "12", "x", "API", "No", "ADVISERS"}
	for _, class := range [][]string{acADV, acSRC, acBE, acHAVE, acSTATE, acNEG, acCONTR, acCRED, acKEY} {
		for _, entry := range class {
			words = append(words, strings.Fields(entry)...)
		}
	}
	return words
}()

var acSeparators = []string{" ", " ", " ", "  ", "-", "_", "'", "’", ". ", ";", "\t", "\n", " ", "\xff", "(", ")", ": ", "K", "é"}

// acRandomReason builds a reason from the vocabulary with random separators
// and random ASCII case, sometimes containing a grammar phrase.
func acRandomReason(random *rand.Rand, phrases []string) string {
	var builder strings.Builder
	count := 1 + random.IntN(12)
	for index := 0; index < count; index++ {
		if index > 0 || random.IntN(4) == 0 {
			builder.WriteString(acSeparators[random.IntN(len(acSeparators))])
		}
		word := acVocabulary[random.IntN(len(acVocabulary))]
		if random.IntN(10) == 0 {
			word = phrases[random.IntN(len(phrases))]
		}
		builder.WriteString(word)
	}
	if random.IntN(4) == 0 {
		builder.WriteString(acSeparators[random.IntN(len(acSeparators))])
	}
	return acRandomCase(random, builder.String())
}

func acRandomCase(random *rand.Rand, s string) string {
	data := []byte(s)
	for index, c := range data {
		if c >= 'a' && c <= 'z' && random.IntN(3) == 0 {
			data[index] = c - ('a' - 'A')
		}
	}
	return string(data)
}

func acAllPhrases() []string {
	var all []string
	for _, phrases := range acPhrases() {
		all = append(all, phrases...)
	}
	slices.Sort(all)
	return all
}

// R1 property over random reasons (fixed seed): claimsNoAdvisers equals the
// independent transcription of the rules, is deterministic, and is invariant
// under ASCII case changes. Fails on any over- or under-match.
func TestClaimsNoAdvisersAgreesWithRulesOnRandomReasons(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(20261108, 4))
	phrases := acAllPhrases()
	positives, failures := 0, 0
	for trial := 0; trial < 20000 && failures < 20; trial++ {
		reason := acRandomReason(random, phrases)
		want := acReferenceClaims(reason)
		if want {
			positives++
		}
		got, ok := acClaimsQuiet(reason)
		if !ok {
			t.Fatalf("R1 violated: claimsNoAdvisers panicked for %q", reason)
		}
		if got != want {
			failures++
			t.Errorf("R1: claimsNoAdvisers(%q) = %v, want %v (normalised %q)", reason, got, want, acNormalise(reason))
			continue
		}
		if again, _ := acClaimsQuiet(reason); again != got {
			failures++
			t.Errorf("R1: claimsNoAdvisers(%q) is not deterministic", reason)
		}
		if swapped, _ := acClaimsQuiet(acASCIIOnlyUpper(reason)); swapped != got {
			failures++
			t.Errorf("R1: ASCII upper-casing %q changed the result", reason)
		}
	}
	if positives < 1000 {
		t.Fatalf("generator produced only %d positive reasons", positives)
	}
}

// acASCIIOnlyUpper upper-cases a-z only and leaves every other byte as is.
func acASCIIOnlyUpper(s string) string {
	data := []byte(s)
	for index, c := range data {
		if c >= 'a' && c <= 'z' {
			data[index] = c - ('a' - 'A')
		}
	}
	return string(data)
}

// R1: invalid UTF-8, control bytes and every single byte value never panic,
// and the result follows the rules. Fails on a panic or on bytes that are not
// treated as separators.
func TestClaimsNoAdvisersArbitraryBytes(t *testing.T) {
	t.Parallel()
	for value := 0; value < 256; value++ {
		b := string([]byte{byte(value)})
		for _, input := range []string{b, "no advisers" + b + "configured", b + "no advisers configured" + b} {
			got, ok := acClaimsQuiet(input)
			if !ok {
				t.Fatalf("R1 violated: claimsNoAdvisers panicked for %q", input)
			}
			if want := acReferenceClaims(input); got != want {
				t.Errorf("R1: claimsNoAdvisers(%q) = %v, want %v", input, got, want)
			}
		}
	}
	random := rand.New(rand.NewPCG(7, 11))
	for trial := 0; trial < 2000; trial++ {
		data := make([]byte, random.IntN(64))
		for index := range data {
			data[index] = byte(random.UintN(256))
		}
		input := string(data)
		got, ok := acClaimsQuiet(input)
		if !ok {
			t.Fatalf("R1 violated: claimsNoAdvisers panicked for %q", input)
		}
		if want := acReferenceClaims(input); got != want {
			t.Errorf("R1: claimsNoAdvisers(%q) = %v, want %v", input, got, want)
		}
	}
}

// R1: long input (at least 64 KiB) never panics and phrases are still found
// at either end. Fails on a length limit, truncation or panic.
func TestClaimsNoAdvisersLongInput(t *testing.T) {
	const size = 64 << 10
	random := rand.New(rand.NewPCG(64, 1024))
	junk := make([]byte, size)
	for index := range junk {
		junk[index] = byte(random.UintN(256))
	}
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"letters_only", strings.Repeat("a", size), false},
		{"separators_only", strings.Repeat("-", size), false},
		{"invalid_utf8_only", strings.Repeat("\xff", size), false},
		{"words_without_phrase", strings.Repeat("adviser checkpoints ", size/20+1), false},
		{"phrase_at_end", strings.Repeat("x ", size/2) + "no advisers configured", true},
		{"phrase_at_start", "no advisers configured " + strings.Repeat("x ", size/2), true},
		{"phrase_after_128KiB", strings.Repeat("K", (2*size)/3+1) + " zero advisers", true},
		{"one_long_word_then_phrase", strings.Repeat("noadvisers", size/10+1) + " no adviser credentials", true},
		{"random_bytes", string(junk), acReferenceClaims(string(junk))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.input) < size {
				t.Fatalf("fixture invariant: input is %d bytes, want at least %d", len(c.input), size)
			}
			if got := acClaims(t, c.input); got != c.want {
				t.Errorf("R1: claimsNoAdvisers(%s, %d bytes) = %v, want %v", c.name, len(c.input), got, c.want)
			}
		})
	}
}

// R1 fuzz target: no panic, deterministic, and agreement with the
// independent transcription of the rules. Seeds are the contract examples.
func FuzzClaimsNoAdvisers(f *testing.F) {
	for _, example := range acTrueExamples {
		f.Add(example.input)
	}
	for _, example := range acFalseExamples {
		f.Add(example.input)
	}
	f.Fuzz(func(t *testing.T, reason string) {
		got := acClaims(t, reason)
		if again := acClaims(t, reason); again != got {
			t.Fatalf("R1: claimsNoAdvisers(%q) is not deterministic", reason)
		}
		if want := acReferenceClaims(reason); got != want {
			t.Fatalf("R1: claimsNoAdvisers(%q) = %v, want %v (normalised %q)", reason, got, want, acNormalise(reason))
		}
	})
}

// ---- R2: Start refuses a contradiction ----

// acPlan is the substantial helper plan with the given checkpoint policy.
func acPlan(t *testing.T, policy *CheckpointPolicy) Plan {
	t.Helper()
	plan := substantialPlan(t)
	plan.Checkpoints = policy
	return plan
}

func acNotRequired(t *testing.T, reason string) Plan {
	t.Helper()
	return acPlan(t, &CheckpointPolicy{Required: false, Reason: reason})
}

// acRepo is a fixture repository with source.txt.
func acRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("first line\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

// acRequireNoTask fails when store holds any entry: a refused Start writes
// nothing in the store (the store directory itself may or may not exist).
func acRequireNoTask(t *testing.T, label, store string) {
	t.Helper()
	if _, err := os.Stat(store); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	if names := taskArtifactNames(t, store); len(names) != 0 {
		t.Fatalf("R2 %s: refused Start left task artifacts in the store: %q", label, names)
	}
}

// acRequireStartRefused starts plan with binding and checks the exact R2
// message and that nothing was written in the store.
func acRequireStartRefused(t *testing.T, label, repo, store string, plan Plan, binding map[string]string, ids string) {
	t.Helper()
	record, err := StartWithPacks(t.Context(), repo, store, plan, binding, SelectionExplicit, nil)
	if err == nil {
		t.Fatalf("R2 %s: Start accepted a contradiction; record %s", label, record.Directory)
	}
	if want := acStartMessage(ids); err.Error() != want {
		t.Fatalf("R2 %s: Start error\n got %q\nwant %q", label, err.Error(), want)
	}
	for _, model := range binding {
		if strings.Contains(err.Error(), model) {
			t.Fatalf("R2 %s: error names the model %q; only IDs may appear: %q", label, model, err.Error())
		}
	}
	acRequireNoTask(t, label, store)
}

// R2: a contradiction is refused with the exact message naming the bound IDs
// sorted ascending and joined with ", ". Fails when Start accepts the plan,
// words the message differently, leaves IDs unsorted or names models.
func TestStartRefusesNoAdvisersClaimWithBoundAdvisers(t *testing.T) {
	for _, c := range []struct {
		name    string
		binding map[string]string
		ids     string
	}{
		{"contract_example", map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}, "openai_decisions, subq_snap"},
		{"single_jev", map[string]string{"jev": "jev-1.14.0"}, "jev"},
		{"single_subq", map[string]string{"subq_snap": "subq-snap-preview"}, "subq_snap"},
		{"all_three", map[string]string{"subq_snap": "subq-snap-preview", "jev": "jev-1.14.0", "openai_decisions": "gpt-6-luna"}, "jev, openai_decisions, subq_snap"},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := filepath.Join(t.TempDir(), "tasks")
			acRequireStartRefused(t, c.name, acRepo(t), store, acNotRequired(t, acClaimReason), c.binding, c.ids)
		})
	}
}

// R2 with R1: every reason for which the rules return true is refused when
// advisers are bound. Fails when Start uses a narrower or different detector.
func TestStartRefusesEveryClaimingReason(t *testing.T) {
	for index, example := range acTrueExamples {
		if !utf8.ValidString(example.input) {
			continue // a plan holds JSON text; invalid UTF-8 is covered at the R1 level
		}
		t.Run(fmt.Sprintf("true_%02d", index), func(t *testing.T) {
			store := filepath.Join(t.TempDir(), "tasks")
			acRequireStartRefused(t, example.input, acRepo(t), store, acNotRequired(t, example.input), fixtureBinding(), acDefaultIDs)
		})
	}
}

// R2 order: the contradiction check runs before the baseline is captured, so
// it precedes baseline-capture and store errors. Fails when Start reports a
// repository or store error instead of the contradiction.
func TestStartContradictionPrecedesBaselineAndStoreErrors(t *testing.T) {
	t.Run("missing_repo", func(t *testing.T) {
		repo := filepath.Join(t.TempDir(), "does-not-exist")
		store := filepath.Join(t.TempDir(), "tasks")
		acRequireStartRefused(t, "missing repo", repo, store, acNotRequired(t, acClaimReason), fixtureBinding(), acDefaultIDs)
	})
	t.Run("repo_is_a_file", func(t *testing.T) {
		repo := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(repo, []byte("not a directory\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		store := filepath.Join(t.TempDir(), "tasks")
		acRequireStartRefused(t, "file repo", repo, store, acNotRequired(t, acClaimReason), fixtureBinding(), acDefaultIDs)
	})
	t.Run("store_inside_repo", func(t *testing.T) {
		repo := acRepo(t)
		store := filepath.Join(repo, "records")
		acRequireStartRefused(t, "store inside repo", repo, store, acNotRequired(t, acClaimReason), fixtureBinding(), acDefaultIDs)
		if _, err := os.Stat(store); err == nil {
			if names := taskArtifactNames(t, store); len(names) != 0 {
				t.Fatalf("R2: refused Start wrote %q inside the repository", names)
			}
		}
	})
	t.Run("control_missing_repo_without_claim_fails_otherwise", func(t *testing.T) {
		// The same missing repository with a non-claiming reason still fails,
		// with an error that is not the contradiction: the order matters.
		repo := filepath.Join(t.TempDir(), "does-not-exist")
		_, err := StartWithPacks(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), acNotRequired(t, acBenignReason), fixtureBinding(), SelectionExplicit, nil)
		if err == nil {
			t.Fatal("fixture invariant: Start accepted a missing repository")
		}
		if strings.Contains(err.Error(), acClaimPrefix) {
			t.Fatalf("R2: a non-claiming reason was reported as a contradiction: %v", err)
		}
	})
}

// R2 order: existing plan validation runs first and keeps its message. Fails
// when the contradiction check runs before (or replaces) existing refusals.
func TestStartExistingPlanValidationPrecedesContradiction(t *testing.T) {
	for name, plan := range map[string]Plan{
		"oversized_claiming_reason": acNotRequired(t, acClaimReason+" "+strings.Repeat("x", 1<<20)),
		"small_plan_with_claiming_checkpoints": func() Plan {
			p := helperPlan(t, "small", "pass")
			p.Checkpoints = &CheckpointPolicy{Required: false, Reason: acClaimReason}
			return p
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			store := filepath.Join(t.TempDir(), "tasks")
			_, err := StartWithPacks(t.Context(), acRepo(t), store, plan, fixtureBinding(), SelectionExplicit, nil)
			if err == nil {
				t.Fatal("R2: Start accepted a plan existing validation refuses")
			}
			if strings.Contains(err.Error(), acClaimPrefix) {
				t.Fatalf("R2: existing validation message replaced by the contradiction: %v", err)
			}
			acRequireNoTask(t, name, store)
		})
	}
}

// R2 unchanged cases: Start succeeds. Fails when Start refuses a plan that is
// not a contradiction (empty or nil binding, non-claiming reason, required
// checkpoints, or no checkpoints member).
func TestStartAcceptsPlansThatAreNotContradictions(t *testing.T) {
	type startCase struct {
		name    string
		plan    Plan
		binding map[string]string
	}
	cases := []startCase{
		{"claim_with_empty_binding", acNotRequired(t, acClaimReason), map[string]string{}},
		{"claim_with_nil_binding", acNotRequired(t, acClaimReason), nil},
		{"required_true_with_claim", acPlan(t, &CheckpointPolicy{Required: true, Reason: acClaimReason}), fixtureBinding()},
		{"required_true_without_reason", acPlan(t, &CheckpointPolicy{Required: true}), fixtureBinding()},
		{"no_checkpoints_member", substantialPlan(t), fixtureBinding()},
		{"owner_reason", acNotRequired(t, acOwnerReason), fixtureBinding()},
	}
	for index, example := range acFalseExamples {
		if strings.TrimSpace(example.input) == "" || !utf8.ValidString(example.input) {
			continue // blank reasons are refused by existing validation; plans hold JSON text
		}
		cases = append(cases, startCase{fmt.Sprintf("false_%02d", index), acNotRequired(t, example.input), fixtureBinding()})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			record, err := StartWithPacks(t.Context(), acRepo(t), filepath.Join(t.TempDir(), "tasks"), c.plan, c.binding, SelectionExplicit, nil)
			if err != nil || record.Directory == "" {
				t.Fatalf("R2 unchanged: Start refused a plan that is not a contradiction: %v", err)
			}
		})
	}
}

// ---- R4: Verify reports a contradiction ----

// acContradictionRecord starts a task with a non-claiming reason, then
// rewrites its stored chain (with valid hash chaining) so the plan holds
// reason, as a record created before R2 or by an older cw would.
func acContradictionRecord(t *testing.T, binding map[string]string, reason string) (Record, SourceRef) {
	t.Helper()
	record, ref := fixtureTaskBound(t, acNotRequired(t, acBenignReason), binding)
	productionRewriteHistory(t, record, func(_ int, historical *Record) {
		historical.Plan.Checkpoints.Reason = reason
	})
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("fixture invariant: rewritten record does not load: %v", err)
	}
	if loaded.Plan.Checkpoints == nil || loaded.Plan.Checkpoints.Required || loaded.Plan.Checkpoints.Reason != reason {
		t.Fatalf("fixture invariant: rewritten checkpoints = %+v", loaded.Plan.Checkpoints)
	}
	return loaded, ref
}

// acFlow describes the evidence attached before Verify.
type acFlow struct {
	before  string // "" (not attached), "resolved", "pending" or "unresolved"
	changed bool   // the source changes after start
	review  bool   // complete an independent review (needed for changed source)
	final   string // "" (not attached), "resolved" or "unresolved"
}

func acMutation(state string) func(*checkpointDraft) {
	switch state {
	case "pending":
		return pendingDecision
	case "unresolved":
		return unresolvedDecision
	}
	return nil
}

// acRunFlow builds the flow on record and returns Verify's result.
func acRunFlow(t *testing.T, record Record, ref SourceRef, flow acFlow) (Verification, error) {
	t.Helper()
	if flow.before != "" {
		attachCheckpoint(t, record, "before", record.Contexts["before"], acMutation(flow.before))
	}
	if flow.changed {
		ref = changedWorkerSource(t, record)
	}
	mustAssess(t, record, ref)
	if flow.review {
		loaded, err := Load(record.Directory)
		if err != nil {
			t.Fatal(err)
		}
		completeIndependentReview(t, loaded, ref, "review")
	}
	if flow.final != "" {
		final, err := PrepareContext(t.Context(), record.Directory, "final")
		if err != nil {
			t.Fatalf("fixture: final context: %v", err)
		}
		attachCheckpoint(t, record, "final", final, acMutation(flow.final))
	}
	return Verify(t.Context(), record.Directory)
}

// acVerifyPair runs flow on a contradiction record and on a control record
// built the same way whose reason does not claim no advisers.
func acVerifyPair(t *testing.T, binding map[string]string, flow acFlow) (contradiction, control Verification, contradictionErr error) {
	t.Helper()
	controlRecord, controlRef := acContradictionRecord(t, binding, acOwnerReason)
	control, _ = acRunFlow(t, controlRecord, controlRef, flow)
	record, ref := acContradictionRecord(t, binding, acClaimReason)
	contradiction, contradictionErr = acRunFlow(t, record, ref, flow)
	return contradiction, control, contradictionErr
}

// R4: each flow without both checkpoints attached gets exactly one problem
// whose text depends on whether a usable before checkpoint exists and on the
// source state; Complete is false and Verify returns an error. Fails when the
// problem is missing, duplicated, worded wrongly, chosen by the wrong
// condition (pending or unresolved before counted as usable, source state
// ignored), or when another problem or limitation changes.
func TestVerifyReportsNoAdvisersClaimWithBoundAdvisers(t *testing.T) {
	t.Parallel()
	all := func(ids string) []string {
		return []string{acVerifyAttachFinal(ids), acVerifyUnchanged(ids), acVerifyChanged(ids)}
	}
	for _, c := range []struct {
		name  string
		flow  acFlow
		want  func(string) string
		clean bool // the control flow completes with no problem
	}{
		{"no_before/unchanged", acFlow{}, acVerifyUnchanged, true},
		{"no_before/changed", acFlow{changed: true, review: true}, acVerifyChanged, true},
		{"resolved_before/unchanged", acFlow{before: "resolved"}, acVerifyAttachFinal, true},
		{"resolved_before/changed", acFlow{before: "resolved", changed: true, review: true}, acVerifyAttachFinal, true},
		{"pending_before/unchanged", acFlow{before: "pending"}, acVerifyUnchanged, false},
		{"unresolved_before/unchanged", acFlow{before: "unresolved"}, acVerifyUnchanged, false},
		{"pending_before/changed", acFlow{before: "pending", changed: true}, acVerifyChanged, false},
		{"unresolved_before/changed", acFlow{before: "unresolved", changed: true}, acVerifyChanged, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			verified, control, err := acVerifyPair(t, fixtureBinding(), c.flow)
			if c.clean && (!control.Complete || len(control.Problems) != 0) {
				t.Fatalf("fixture invariant: control flow is not clean: %+v", control)
			}
			want := c.want(acDefaultIDs)
			if got := acCount(verified.Problems, want); got != 1 {
				t.Fatalf("R4: problem %q occurs %d times, want once; problems %q", want, got, verified.Problems)
			}
			for _, other := range all(acDefaultIDs) {
				if other != want && acCount(verified.Problems, other) != 0 {
					t.Fatalf("R4: wrong variant %q reported; problems %q", other, verified.Problems)
				}
			}
			if got := acCountContaining(verified.Problems, acClaimPrefix); got != 1 {
				t.Fatalf("R4: %d contradiction problems, want exactly one: %q", got, verified.Problems)
			}
			if len(verified.Problems) != len(control.Problems)+1 {
				t.Fatalf("R4: problems %q, want the control's %q plus one", verified.Problems, control.Problems)
			}
			if verified.Complete {
				t.Fatal("R4: Complete is true with a contradiction problem")
			}
			if err == nil {
				t.Fatal("R4: Verify returned no error for incomplete evidence")
			}
			limitation := "adviser checkpoints not required: " + acClaimReason
			if got := acCount(verified.Limitations, limitation); got != 1 {
				t.Fatalf("R4: limitation %q occurs %d times, want once: %q", limitation, got, verified.Limitations)
			}
			if len(verified.Limitations) != len(control.Limitations) {
				t.Fatalf("R4: limitations changed: %q, control %q", verified.Limitations, control.Limitations)
			}
		})
	}
}

// R4 Terms: the message names the bound IDs of Record.Advisers sorted
// ascending and joined with ", ", and never a model name. Fails on unsorted,
// differently joined or model-bearing text.
func TestVerifyNoAdvisersClaimNamesSortedBoundIDs(t *testing.T) {
	for _, c := range []struct {
		name    string
		binding map[string]string
		ids     string
	}{
		{"jev_only", map[string]string{"jev": "jev-1.14.0"}, "jev"},
		{"all_three", map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna", "jev": "jev-1.14.0"}, "jev, openai_decisions, subq_snap"},
	} {
		t.Run(c.name, func(t *testing.T) {
			record, ref := acContradictionRecord(t, c.binding, acClaimReason)
			verified, err := acRunFlow(t, record, ref, acFlow{})
			if err == nil || verified.Complete {
				t.Fatalf("R4: contradiction verified complete: %+v %v", verified, err)
			}
			if got := acCount(verified.Problems, acVerifyUnchanged(c.ids)); got != 1 {
				t.Fatalf("R4: problem for IDs %q occurs %d times: %q", c.ids, got, verified.Problems)
			}
			for _, problem := range verified.Problems {
				for _, model := range c.binding {
					if strings.Contains(problem, model) {
						t.Fatalf("R4: problem names the model %q: %q", model, problem)
					}
				}
			}
		})
	}
}

// R4 exception: with both before and final attached the problem is not added
// and the attached checkpoints are validated as today. Fails when the
// contradiction is reported despite both checkpoints, or when it hides or
// changes the existing validation of an invalid final checkpoint.
func TestVerifyNoAdvisersClaimWithBothCheckpointsAttached(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		flow     acFlow
		complete bool
	}{
		{"both_resolved/unchanged", acFlow{before: "resolved", final: "resolved"}, true},
		{"both_resolved/changed", acFlow{before: "resolved", changed: true, review: true, final: "resolved"}, true},
		{"unresolved_final", acFlow{before: "resolved", final: "unresolved"}, false},
		{"pending_final", acFlow{before: "resolved", final: "pending"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			verified, control, err := acVerifyPair(t, fixtureBinding(), c.flow)
			if control.Complete != c.complete {
				t.Fatalf("fixture invariant: control Complete = %v, want %v: %+v", control.Complete, c.complete, control)
			}
			if got := acCountContaining(verified.Problems, acClaimPrefix); got != 0 {
				t.Fatalf("R4: contradiction reported although both checkpoints are attached: %q", verified.Problems)
			}
			if verified.Complete != c.complete || len(verified.Problems) != len(control.Problems) {
				t.Fatalf("R4: both-attached result changed: %+v, control %+v", verified, control)
			}
			if c.complete && err != nil {
				t.Fatalf("R4: complete verification returned an error: %v", err)
			}
		})
	}
}

// R4: the verification with the contradiction is recorded like any other
// incomplete verification. Fails when Verify skips recording it.
func TestVerifyRecordsTheContradictionVerification(t *testing.T) {
	record, ref := acContradictionRecord(t, fixtureBinding(), acClaimReason)
	verified, err := acRunFlow(t, record, ref, acFlow{})
	if err == nil || verified.Complete {
		t.Fatalf("R4: contradiction verified complete: %+v %v", verified, err)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Verification == nil || loaded.Verification.Complete || acCount(loaded.Verification.Problems, acVerifyUnchanged(acDefaultIDs)) != 1 {
		t.Fatalf("R4: stored verification does not hold the problem: %+v", loaded.Verification)
	}
}

// R4: a verification stored by an earlier Verify call is never rewritten;
// only a new Verify call evaluates the rule. Fails when Load re-evaluates the
// stored verification or Verify alters earlier revisions.
func TestVerifyDoesNotRewriteStoredVerification(t *testing.T) {
	record, ref := fixtureTaskBound(t, acNotRequired(t, acBenignReason), fixtureBinding())
	mustAssess(t, record, ref)
	if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
		t.Fatalf("fixture invariant: non-claiming record did not verify: %+v %v", verified, err)
	}
	stored, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	productionRewriteHistory(t, stored, func(_ int, historical *Record) {
		historical.Plan.Checkpoints.Reason = acClaimReason
	})
	old, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("fixture invariant: rewritten record does not load: %v", err)
	}
	if old.Verification == nil || !old.Verification.Complete || acCountContaining(old.Verification.Problems, acClaimPrefix) != 0 {
		t.Fatalf("R4: the stored verification was re-evaluated on load: %+v", old.Verification)
	}
	files := productionTaskBytes(t, record.Directory)
	verified, err := Verify(t.Context(), record.Directory)
	if err == nil || verified.Complete || acCount(verified.Problems, acVerifyUnchanged(acDefaultIDs)) != 1 {
		t.Fatalf("R4: a new Verify did not report the contradiction: %+v %v", verified, err)
	}
	after := productionTaskBytes(t, record.Directory)
	for name, data := range files {
		if strings.HasPrefix(name, "record-") && after[name] != data {
			t.Fatalf("R4: Verify rewrote the earlier revision %s", name)
		}
	}
}

// R4: an abandoned record is still refused by Verify. Fails when the rule
// changes which records Verify accepts.
func TestVerifyStillRefusesAbandonedContradiction(t *testing.T) {
	record, _ := acContradictionRecord(t, fixtureBinding(), acClaimReason)
	if _, err := Abandon(record.Directory, "superseded by a corrected record"); err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(t.Context(), record.Directory)
	if err == nil || verified.Complete {
		t.Fatalf("R4: Verify accepted an abandoned record: %+v %v", verified, err)
	}
}

// R4 condition Checkpoints.Required == false: with required checkpoints a
// claiming reason is not a contradiction, so Verify adds no R4 problem, with
// no before checkpoint or with only a resolved before checkpoint (the cases
// the both-attached exception does not cover). Fails when Verify reports the
// contradiction without checking Required, or changes any other problem.
func TestVerifyRequiredCheckpointsWithClaimingReasonAddNoProblem(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		flow acFlow
	}{
		{"no_before", acFlow{}},
		{"resolved_before_no_final", acFlow{before: "resolved"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			controlRecord, controlRef := fixtureTaskBound(t, acPlan(t, &CheckpointPolicy{Required: true, Reason: acOwnerReason}), fixtureBinding())
			control, _ := acRunFlow(t, controlRecord, controlRef, c.flow)
			record, ref := fixtureTaskBound(t, acPlan(t, &CheckpointPolicy{Required: true, Reason: acClaimReason}), fixtureBinding())
			verified, _ := acRunFlow(t, record, ref, c.flow)
			if got := acCountContaining(verified.Problems, acClaimPrefix); got != 0 {
				t.Fatalf("R4: required checkpoints reported as a contradiction: %q", verified.Problems)
			}
			if len(verified.Problems) != len(control.Problems) || verified.Complete != control.Complete {
				t.Fatalf("R4: required checkpoints with a claiming reason changed the result: %+v, control %+v", verified, control)
			}
		})
	}
}

// ---- R5: unchanged behaviour ----

// R5: an empty binding with a claiming reason starts, and verify adds no new
// problem while keeping both limitations. Fails when the rule fires on an
// empty binding or drops either limitation.
func TestNoAdvisersClaimWithEmptyBindingIsUnchanged(t *testing.T) {
	record, ref := fixtureTaskBound(t, acNotRequired(t, acClaimReason), map[string]string{})
	mustAssess(t, record, ref)
	verified, err := Verify(t.Context(), record.Directory)
	if err != nil || !verified.Complete || len(verified.Problems) != 0 {
		t.Fatalf("R5: empty-binding task with a claiming reason: %+v %v", verified, err)
	}
	for _, limitation := range []string{
		"no advisers were configured when the task started; decisions rest on source evidence and checks only",
		"adviser checkpoints not required: " + acClaimReason,
	} {
		if got := acCount(verified.Limitations, limitation); got != 1 {
			t.Fatalf("R5: limitation %q occurs %d times: %q", limitation, got, verified.Limitations)
		}
	}
}

// R5: a non-claiming reason with bound advisers verifies as before. Fails
// when the rule fires on reasons the heuristic does not match.
func TestNonClaimingReasonWithBoundAdvisersIsUnchanged(t *testing.T) {
	for _, reason := range []string{acOwnerReason, acBenignReason} {
		t.Run(reason, func(t *testing.T) {
			record, ref := fixtureTask(t, acNotRequired(t, reason))
			mustAssess(t, record, ref)
			verified, err := Verify(t.Context(), record.Directory)
			if err != nil || !verified.Complete || len(verified.Problems) != 0 {
				t.Fatalf("R5: non-claiming reason: %+v %v", verified, err)
			}
			if got := acCount(verified.Limitations, "adviser checkpoints not required: "+reason); got != 1 {
				t.Fatalf("R5: not-required limitation occurs %d times: %q", got, verified.Limitations)
			}
		})
	}
}

// R5: required checkpoints whose attached consultations are unavailable
// (HTTP 429) or partial complete as before, with the coverage limitation and
// no problem, even when the reason text claims no advisers. Fails when such
// coverage outcomes become problems.
func TestRequiredCheckpointsWithUnavailableCoverageAreUnchanged(t *testing.T) {
	t.Parallel()
	unavailable := withOutcomes(map[string]adviserOutcome{
		"subq_snap":        {Status: "unavailable", Category: "http_error", HTTPStatus: 429},
		"openai_decisions": {Status: "unavailable", Category: "http_error", HTTPStatus: 429},
	})
	partial := func(draft *checkpointDraft) {
		draft.Questions = []string{"claim", "other"}
		draft.Outcomes = map[string]adviserOutcome{
			"subq_snap":        {Status: "success", Choices: map[string]string{"claim": "supported", "other": "supported"}},
			"openai_decisions": {Status: "partial", Choices: map[string]string{"claim": "supported"}},
		}
	}
	for _, c := range []struct {
		name   string
		policy *CheckpointPolicy
		mutate func(*checkpointDraft)
		words  []string
	}{
		{"required_unavailable_429", &CheckpointPolicy{Required: true}, unavailable, []string{"subq_snap", "unavailable"}},
		{"required_with_claim_reason_unavailable_429", &CheckpointPolicy{Required: true, Reason: acClaimReason}, unavailable, []string{"subq_snap", "unavailable"}},
		{"omitted_unavailable_429", nil, unavailable, []string{"openai_decisions", "unavailable"}},
		{"required_partial", &CheckpointPolicy{Required: true}, partial, []string{"openai_decisions"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			record, ref := fixtureTask(t, acPlan(t, c.policy))
			attachCheckpoint(t, record, "before", record.Contexts["before"], c.mutate)
			mustAssess(t, record, ref)
			final, err := PrepareContext(t.Context(), record.Directory, "final")
			if err != nil {
				t.Fatal(err)
			}
			attachCheckpoint(t, record, "final", final, c.mutate)
			verified, err := Verify(t.Context(), record.Directory)
			if err != nil || !verified.Complete || len(verified.Problems) != 0 {
				t.Fatalf("R5: coverage outcome became a problem: %+v %v", verified, err)
			}
			if !limitationNaming(verified.Limitations, c.words...) {
				t.Fatalf("R5: coverage limitation missing: %q", verified.Limitations)
			}
			if acCountContaining(verified.Limitations, acClaimPrefix) != 0 {
				t.Fatalf("R5: contradiction text in limitations: %q", verified.Limitations)
			}
		})
	}
}
