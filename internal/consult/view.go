package consult

import (
	"errors"
	"strconv"
)

// RecordView is a compact projection of a consultation record that this run
// has just written. It names the record and the request by path and digest and
// never repeats the request's state or questions, which the coordinator wrote
// itself. The record on disk is unchanged and stays the evidence.
type RecordView struct {
	Record        string                    `json:"record"`
	Status        string                    `json:"status"`
	Request       RequestView               `json:"request"`
	Providers     map[string]ProviderView   `json:"providers"`
	Comparison    map[string]ComparisonView `json:"comparison,omitempty"`
	FinalDecision DecisionView              `json:"final_decision"`
}

// RequestView identifies the shared request. File is the --request path, or
// null for an adjudication, whose request came from SourceRecord.
type RequestView struct {
	File         *string `json:"file"`
	SourceRecord string  `json:"source_record,omitempty"`
	SHA256       string  `json:"sha256"`
}

// ProviderView is one adviser's outcome: answers when it answered, otherwise
// the error category recorded for it.
type ProviderView struct {
	Status         string                `json:"status"`
	RequestedModel string                `json:"requested_model"`
	ResolvedModel  string                `json:"resolved_model,omitempty"`
	Answers        map[string]AnswerView `json:"answers,omitempty"`
	Error          *ErrorView            `json:"error,omitempty"`
}

// AnswerView is one adviser answer; Refusal is set instead of a choice when
// the adviser declined the question.
type AnswerView struct {
	Choice        string             `json:"choice,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Refusal       bool               `json:"refusal,omitzero"`
}

// ErrorView is a recorded provider failure category.
type ErrorView struct {
	Category   string `json:"category"`
	HTTPStatus int    `json:"http_status,omitzero"`
}

// ComparisonView is the record's per-question comparison of adviser choices.
type ComparisonView struct {
	Status  string            `json:"status"`
	Choices map[string]string `json:"choices"`
	Missing map[string]string `json:"missing"`
}

// DecisionView is the coordinator decision status and, per question, its
// status and choice; reasons and evidence stay in the record.
type DecisionView struct {
	Status    string                  `json:"status"`
	Questions map[string]QuestionView `json:"questions,omitempty"`
}

// QuestionView is one question's coordinator decision.
type QuestionView struct {
	Status string `json:"status"`
	Choice string `json:"choice,omitempty"`
}

var errView = errors.New("written consultation record cannot be summarized")

// View reads the record that Run wrote to o.Output and returns its compact
// projection. Call it only after Run returned status 0 or 2, when o.Output is
// the record this run created.
func (o Options) View() (RecordView, error) {
	value, err := readJSON(o.Output)
	if err != nil {
		return RecordView{}, errView
	}
	record, ok := mapping(value)
	if !ok {
		return RecordView{}, errView
	}
	view := RecordView{Record: o.Output, Providers: map[string]ProviderView{}}
	view.Status, _ = text(record.get("status"))
	view.Request.SHA256, _ = text(record.get("request_sha256"))
	if o.Adjudicate != "" {
		view.Request.SourceRecord = o.Adjudicate
	} else {
		file := o.Request
		view.Request.File = &file
	}
	providers, ok := mapping(record.get("providers"))
	if !ok {
		return RecordView{}, errView
	}
	for _, name := range providers.names {
		provider, ok := mapping(providers.get(name))
		if !ok {
			return RecordView{}, errView
		}
		view.Providers[name] = providerView(provider)
	}
	// A dry-run preview has no comparison.
	if compared, ok := mapping(record.get("comparison")); ok {
		view.Comparison = map[string]ComparisonView{}
		for _, id := range compared.names {
			item, ok := mapping(compared.get(id))
			if !ok {
				return RecordView{}, errView
			}
			entry := ComparisonView{Choices: texts(item.get("choices")), Missing: texts(item.get("missing"))}
			entry.Status, _ = text(item.get("status"))
			view.Comparison[id] = entry
		}
	}
	decision, ok := mapping(record.get("final_decision"))
	if !ok {
		return RecordView{}, errView
	}
	view.FinalDecision.Status, _ = text(decision.get("status"))
	if questions, ok := mapping(decision.get("questions")); ok && questions.size() > 0 {
		view.FinalDecision.Questions = map[string]QuestionView{}
		for _, id := range questions.names {
			item, ok := mapping(questions.get(id))
			if !ok {
				return RecordView{}, errView
			}
			var question QuestionView
			question.Status, _ = text(item.get("status"))
			question.Choice, _ = text(item.get("choice"))
			view.FinalDecision.Questions[id] = question
		}
	}
	return view, nil
}

func providerView(provider *object) ProviderView {
	var view ProviderView
	view.Status, _ = text(provider.get("status"))
	view.RequestedModel, _ = text(provider.get("requested_model"))
	view.ResolvedModel, _ = text(provider.get("resolved_model"))
	if failure, ok := mapping(provider.get("error")); ok {
		view.Error = &ErrorView{}
		view.Error.Category, _ = text(failure.get("category"))
		if code, ok := failure.get("http_status").(integer); ok {
			view.Error.HTTPStatus, _ = strconv.Atoi(string(code))
		}
	}
	result, _ := mapping(provider.get("result"))
	if result == nil {
		return view
	}
	answers, _ := mapping(result.get("answers"))
	if answers == nil {
		return view
	}
	view.Answers = map[string]AnswerView{}
	for _, id := range answers.names {
		answer, ok := mapping(answers.get(id))
		if !ok {
			continue
		}
		var item AnswerView
		if answer.get("type") != "choice" {
			item.Refusal = true
			view.Answers[id] = item
			continue
		}
		item.Choice, _ = text(answer.get("choice"))
		if confidence, ok := asFloat(answer.get("confidence")); ok {
			item.Confidence = &confidence
		}
		if probabilities, ok := mapping(answer.get("probabilities")); ok {
			item.Probabilities = map[string]float64{}
			for _, option := range probabilities.names {
				if value, ok := asFloat(probabilities.get(option)); ok {
					item.Probabilities[option] = value
				}
			}
		}
		view.Answers[id] = item
	}
	return view
}

func asFloat(value any) (float64, bool) {
	switch item := value.(type) {
	case float64:
		return item, true
	case integer:
		return item.float(), true
	}
	return 0, false
}

func texts(value any) map[string]string {
	result := map[string]string{}
	if items, ok := mapping(value); ok {
		for _, name := range items.names {
			if item, ok := text(items.get(name)); ok {
				result[name] = item
			}
		}
	}
	return result
}
