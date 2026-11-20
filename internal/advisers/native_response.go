package advisers

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

type nativeAnswer struct {
	Type          string              `json:"type"`
	Choice        string              `json:"choice"`
	Score         *float64            `json:"score"`
	Noul          *float64            `json:"noul"`
	Confidence    *float64            `json:"confidence"`
	Probabilities map[string]*float64 `json:"probabilities"`
	Legend        map[string]string   `json:"legend"`
}

func decodeNativeResponse(data []byte, r Request) (*DecisionResult, string, *Usage, error) {
	answers, model, usage, err := decodeNativeAnswers(data, map[string]Request{"result": r})
	return answers["result"], model, usage, err
}

func decodeNativeAnswers(data []byte, requests map[string]Request) (map[string]*DecisionResult, string, *Usage, error) {
	invalid := errors.New("invalid native response")
	if len(data) > MaxAnswer || !utf8.Valid(data) || jsonKeys(json.NewDecoder(bytes.NewReader(data)), 0, false) != nil {
		return nil, "", nil, invalid
	}
	var response struct {
		Model   *string                 `json:"model"`
		Answers map[string]nativeAnswer `json:"answers"`
		Usage   *struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &response) != nil || len(response.Answers) != len(requests) {
		return nil, "", nil, invalid
	}
	results := make(map[string]*DecisionResult, len(requests))
	for name, request := range requests {
		answer, exists := response.Answers[name]
		if !exists {
			return nil, "", nil, invalid
		}
		result, err := normalizeNativeAnswer(answer, request, len(requests) > 1)
		if err != nil {
			return nil, "", nil, err
		}
		results[name] = result
	}
	model := "unknown"
	if response.Model != nil {
		model = *response.Model
		if strings.TrimSpace(model) == "" || strings.ContainsRune(model, 0) {
			return nil, "", nil, invalid
		}
	}
	var usage *Usage
	if u := response.Usage; u != nil {
		if u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output < 0 || *u.Input > math.MaxInt64-*u.Output {
			return nil, "", nil, invalid
		}
		usage = &Usage{InputTokens: *u.Input, OutputTokens: *u.Output}
	}
	return results, model, usage, nil
}

func normalizeNativeAnswer(a nativeAnswer, r Request, optionalConfidence bool) (*DecisionResult, error) {
	invalid := errors.New("answer does not match the requested decision")
	result := &DecisionResult{Kind: r.Kind}
	if r.Kind == "boolean" {
		if a.Type != "noul" || !probability(a.Noul) || a.Score != nil || a.Choice != "" || a.Confidence != nil || len(a.Probabilities) != 0 || len(a.Legend) != 0 {
			return nil, invalid
		}
		result.ProbabilityTrue = a.Noul
		return result, nil
	}
	if ((!optionalConfidence || a.Confidence != nil) && !probability(a.Confidence)) || a.Noul != nil {
		return nil, invalid
	}
	result.Confidence = a.Confidence
	var ids []string
	var err error
	switch r.Kind {
	case "decision":
		ids, err = nativeChoiceIDs(a, r.Choices)
		result.Choice = a.Choice
	case "score":
		ids, err = nativeScoreIDs(a, r.Levels)
		result.Score = a.Score
	default:
		return nil, invalid
	}
	if err != nil {
		return nil, err
	}
	probabilities, err := normalizeProbabilities(a.Probabilities, ids)
	if err != nil {
		return nil, err
	}
	result.Probabilities = probabilities
	return result, nil
}

func nativeChoiceIDs(a nativeAnswer, choices []Choice) ([]string, error) {
	invalid := errors.New("answer does not match the requested choices")
	if a.Type != "choice" || a.Score != nil || len(a.Legend) != 0 {
		return nil, invalid
	}
	selected, ok := a.Probabilities[a.Choice]
	if !ok || !probability(selected) {
		return nil, invalid
	}
	for _, p := range a.Probabilities {
		if !probability(p) || *p > *selected+1e-9 {
			return nil, invalid
		}
	}
	ids := make([]string, 0, len(choices))
	for _, c := range choices {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

func nativeScoreIDs(a nativeAnswer, levels []string) ([]string, error) {
	invalid := errors.New("answer does not match the requested score levels")
	if a.Type != "score" || a.Choice != "" || a.Score == nil || !finite(*a.Score) || *a.Score < 0 || *a.Score > float64(len(levels)-1) || len(a.Legend) != len(levels) {
		return nil, invalid
	}
	ids := make([]string, 0, len(levels))
	for i, level := range levels {
		id := strconv.Itoa(i)
		if a.Legend[id] != level {
			return nil, invalid
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func normalizeProbabilities(values map[string]*float64, ids []string) ([]Probability, error) {
	invalid := errors.New("invalid probability distribution")
	if len(values) != len(ids) || len(ids) == 0 {
		return nil, invalid
	}
	result := make([]Probability, 0, len(ids))
	total := 0.0
	for _, id := range ids {
		p := values[id]
		if !probability(p) {
			return nil, invalid
		}
		total += *p
		result = append(result, Probability{ID: id, Probability: *p})
	}
	// Providers may round the individual values; never renormalize their answer.
	if math.Abs(total-1) > 0.01 {
		return nil, invalid
	}
	return result, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func probability(value *float64) bool {
	return value != nil && finite(*value) && *value >= 0 && *value <= 1
}
