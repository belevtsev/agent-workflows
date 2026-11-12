package taskflow

import (
	"context"
	"errors"
	"os"
	"time"
)

// Abandonment marks a record that its coordinator gave up. The revision that
// sets it is the record's last: no later revision is written or loaded, and
// an abandoned record never gates publication.
type Abandonment struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// errAbandoned refuses every change to an abandoned record.
var errAbandoned = errors.New("task was abandoned; start a new task with 'cw task start'")

// Abandon appends the terminal revision that marks the record at path
// abandoned for reason, a non-blank text of at most 16 KiB. It takes no
// context, so it waits for the task lock for at most its 10 s bound.
func Abandon(path, reason string) (Record, error) {
	if !boundedText(reason) {
		return Record{}, errors.New("an abandonment reason of 1 to 16384 bytes is required")
	}
	return update(context.Background(), path, func(_ *os.Root, record *Record) error {
		record.Abandonment = &Abandonment{Reason: reason, At: time.Now().UTC()}
		return nil
	})
}

// validateAbandonment checks a stored abandonment's shape.
func validateAbandonment(record Record) error {
	if abandonment := record.Abandonment; abandonment != nil && (!boundedText(abandonment.Reason) || abandonment.At.IsZero()) {
		return errors.New("invalid stored abandonment")
	}
	return nil
}
