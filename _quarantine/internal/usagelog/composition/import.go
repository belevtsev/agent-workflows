package composition

import (
	"context"
	"fmt"

	"github.com/belevtsev/agent-workflows/v3/internal/usagelog"
)

// Report is a usage import report with its context composition. It encodes
// as the usagelog report with a "composition" member last.
type Report struct {
	usagelog.Report
	Composition *Composition `json:"composition,omitempty"`
}

// Import is usagelog.Import plus the composition, which needs a price table
// to reconcile with the recorded cost. A Codex rollout records no cost, so
// its composition is unsupported, or withheld when the import is incomplete.
func Import(ctx context.Context, options usagelog.Options) (Report, error) {
	if err := usagelog.CheckOptions(options); err != nil {
		return Report{}, err
	}
	if options.Prices == nil {
		return Report{}, fmt.Errorf("%w: a composition needs a price table to reconcile with the recorded cost", usagelog.ErrInvalidOptions)
	}
	c := newComposer(options.Prices)
	imported, err := usagelog.ImportObserved(ctx, options, c)
	if err != nil {
		return Report{}, err
	}
	report := Report{Report: imported}
	if report.Source == usagelog.SourceCodex {
		report.Composition = unsupportedComposition(report)
	} else {
		report.Composition = c.build(&report)
	}
	return report, nil
}

// Session implements usagelog.Observer.
func (c *composer) Session(id string) { c.session = id }

// Agent implements usagelog.Observer.
func (c *composer) Agent(agent usagelog.Agent, main bool) usagelog.LineObserver {
	return agentLines{c, c.addAgent(agent, main)}
}

// agentLines passes one transcript file's lines to the composer.
type agentLines struct {
	c *composer
	a *compAgent
}

func (l agentLines) Assistant(line []byte, key, model string, tokens usagelog.Tokens, recorded, history bool) {
	l.c.assistant(l.a, line, key, model, tokens, recorded, history)
}

func (l agentLines) User(line []byte)       { l.c.user(l.a, line) }
func (l agentLines) Attachment(line []byte) { l.c.attachment(l.a, line) }
func (l agentLines) System(line []byte)     { l.c.system(l.a, line) }
