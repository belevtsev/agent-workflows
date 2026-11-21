package advisers

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

type StoryInput struct {
	Request  string           `json:"request"`
	Goal     string           `json:"goal"`
	Scope    string           `json:"scope"`
	Criteria []StoryCriterion `json:"criteria,omitempty"`
}

type StoryCriterion struct {
	ID       string `json:"id"`
	Expected string `json:"expected_behavior"`
}

const StoryModel = "jev-1.14.0"

func PrepareStoryReview(d Definition, story StoryInput) (*NativeCall, error) {
	if d.Provider != "typesafe" || d.Model != StoryModel {
		return nil, errors.New("story review requires the pinned TypeSafe Jev model " + StoryModel)
	}
	text, err := json.Marshal(story)
	if err != nil || len(text) > 16<<10 || !utf8.ValidString(story.Request) {
		return nil, errors.New("story review needs a UTF-8 story up to 16 KiB")
	}
	d.Timeout = min(d.Timeout, 10*time.Second)
	// Treat the story as data. These six independent questions share its state.
	choices := []Choice{
		{ID: "clear", Text: "The story gives specific details for this aspect."},
		{ID: "gap", Text: "This aspect matters but the story omits a necessary detail."},
		{ID: "uncertain", Text: "The text is ambiguous or gives contradictory details for this aspect."},
		{ID: "not_applicable", Text: "This aspect does not affect the requested behavior."},
	}
	questions := map[string]string{
		"goal_actor":    "How clearly does this story identify who needs the change and what they need to accomplish?",
		"scope":         "How clearly does this story delimit the requested change?",
		"behavior":      "How clearly does this story describe the expected observable behavior?",
		"testability":   "How well can the acceptance criteria be checked with repeatable examples?",
		"failure_cases": "How clearly does this story describe relevant empty, invalid or failing cases?",
		"compatibility": "How clearly does this story describe relevant dependencies or compatibility requirements?",
	}
	requests := make(map[string]Request, len(questions))
	for id, question := range questions {
		requests[id] = Request{Version: 2, Kind: "decision", Question: question, Choices: choices}
	}
	call, err := prepareNative(d, requests, string(text))
	if err == nil && responseContainsCredential(call.body, call.key) {
		return nil, errors.New("story review input contained the adviser credential")
	}
	return call, err
}
