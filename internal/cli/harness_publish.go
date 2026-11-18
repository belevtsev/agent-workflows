package cli

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
)

var errPublicationUnconfirmed = errors.New("publication remains unconfirmed")

func confirmHarness(ctx context.Context, s *session, t *harness.Task, o harnessOptions, v harnessView) error {
	recheck := v.Next == "done" && t.Scope.Publish != ""
	if recheck {
		e := t.Latest("publish")
		if e == nil || o.ticket != "" && o.ticket != e.Ticket || o.note != "" && o.note != e.Link {
			return failf("confirmation differs from the saved publication receipt")
		}
		o.ticket, o.note = e.Ticket, e.Link
	} else if v.Next != "publish" || v.Ticket != o.ticket || o.ticket == "" {
		return failf("publication needs explicit scope and a current ticket")
	}
	if t.Scope.PRBase != "" && !harnessPRURL.MatchString(o.note) {
		return usagef("provide the HTTPS pull request URL with --pr")
	}
	if t.Scope.PRBase == "" && o.note != "" {
		return usagef("--pr needs a requested --pr-base")
	}
	err := observePublication(ctx, s, t.Scope, o.note, v.Candidate.Head)
	event := harness.Event{Action: "publish", Candidate: v.Candidate, Note: t.Scope.Publish, Link: o.note, Ticket: o.ticket, At: time.Now().UTC()}
	if err != nil {
		if recheck {
			// A live recheck records a new observation without changing the delivery.
			event.Action = "publication_unconfirmed"
			t.Events = append(t.Events, event)
		}
		return errors.Join(errPublicationUnconfirmed, err)
	}
	if recheck {
		refreshPublication(t, event)
	} else {
		t.Events = append(t.Events, event)
		t.Completion = v.deliveryCompletion(event.At)
	}
	return nil
}

func refreshPublication(t *harness.Task, event harness.Event) {
	if latest := t.Latest("publish", "publication_unconfirmed"); latest != nil && latest.Action == "publish" {
		latest.ReconfirmedAt = event.At
		return
	}
	t.Events = append(t.Events, event)
}

func observePublication(ctx context.Context, s *session, scope harness.Scope, link, want string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	remote, branch, _ := strings.Cut(scope.Publish, "/")
	return s.repo.ConfirmPublication(ctx, remote, branch, want, scope.PRBase, link)
}

// A saved observation identifies the exact publication it describes. Local
// state views can present it without inspecting a remote or exposing its URL credentials.
type publicationReceipt struct {
	Outcome    string    `json:"outcome"`
	Target     string    `json:"target"`
	Commit     string    `json:"commit"`
	PR         string    `json:"pull_request,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

func lastPublication(t *harness.Task) *publicationReceipt {
	if t.Scope.Publish == "" {
		return nil
	}
	e := t.Latest("publish", "publication_unconfirmed")
	if e == nil {
		return nil
	}
	outcome := "confirmed"
	if e.Action == "publication_unconfirmed" {
		outcome = "unconfirmed"
	}
	observed := e.At
	if !e.ReconfirmedAt.IsZero() {
		observed = e.ReconfirmedAt
	}
	return &publicationReceipt{Outcome: outcome, Target: e.Note, Commit: e.Candidate.Head, PR: e.Link, ObservedAt: observed}
}

// gh accepts HTTPS pull URLs for github.com and enterprise hosts.
var harnessPRURL = regexp.MustCompile(`^https://[a-zA-Z0-9.-]+(:[0-9]+)?/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+/pull/[1-9][0-9]*$`)

func extendDeliveryScope(scope *harness.Scope, extension harness.Scope) error {
	if extension.Publish != "" {
		if scope.Publish != "" && scope.Publish != extension.Publish {
			return usagef("publication destination already bound")
		}
		scope.Publish = extension.Publish
	}
	if extension.PRBase != "" {
		if scope.PRBase != "" && scope.PRBase != extension.PRBase {
			return usagef("PR base already bound")
		}
		scope.PRBase = extension.PRBase
	}
	if err := scope.Validate(); err != nil {
		return usagef("%v", err)
	}
	return nil
}
