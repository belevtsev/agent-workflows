package advisers

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

func validateRequestKind(r Request) error {
	if r.Version != 1 && r.Version != 2 {
		return errors.New("adviser request version must be 1 or 2")
	}
	if !slices.Contains([]string{"advice", "decision", "score", "boolean"}, r.Kind) {
		return errors.New("adviser request kind must be advice, decision, score or boolean")
	}
	if r.Version == 1 && (r.Kind == "score" || r.Kind == "boolean" || r.Levels != nil || r.Criteria != nil) {
		return errors.New("score, boolean, levels and criteria require request version 2")
	}
	if r.Kind != "score" && r.Levels != nil {
		return errors.New("only score requests may contain levels")
	}
	if r.Kind != "boolean" && r.Criteria != nil {
		return errors.New("only boolean requests may contain criteria")
	}
	if r.Kind == "score" {
		return validateLevels(r.Levels)
	}
	if r.Criteria != nil && (!utf8.ValidString(r.Criteria.True) || !utf8.ValidString(r.Criteria.False)) {
		return errors.New("boolean criteria must be UTF-8 text")
	}
	return nil
}

func validateLevels(levels []string) error {
	if len(levels) < 2 || len(levels) > 10 {
		return errors.New("score requests need 2 to 10 ordered levels")
	}
	for _, level := range levels {
		if strings.TrimSpace(level) == "" || !utf8.ValidString(level) {
			return errors.New("score levels must be nonempty UTF-8 descriptions")
		}
	}
	return nil
}
