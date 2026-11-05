package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/workflow"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

func (w *setupWizard) width() int {
	width := 76
	if f, ok := w.config.Err.(*os.File); ok {
		if n, _, err := term.GetSize(int(f.Fd())); err == nil && n > 0 {
			width = min(width, n-2)
		}
	}
	return max(width, 20)
}

func (w *setupWizard) header(v workflow.ConfigurationView) {
	if w.plain {
		w.print("\nAGENT WORKFLOWS · SETUP\nRelease %s\nEnter keeps the selected default. Ctrl-C cancels.\n\n", v.Target.Version)
		return
	}
	accent := lipgloss.Color("#B9A2FF")
	logo := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("◈  AGENT WORKFLOWS")
	sub := lipgloss.NewStyle().Foreground(lipgloss.Color("#A7B1C2")).Render("Your agents. Your defaults.\nA guided setup for Codex and Claude Code.")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2).Width(w.width() - 2)
	w.print("%s\n", box.Render(logo+"\n\n"+sub+"\n\nRelease "+v.Target.Version))
	w.print("%s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#798497")).Width(w.width()).Render("01 Hosts  →  02 Models  →  03 Keys  →  04 Review"))
}

// Each selector owns and restores its terminal mode. Importing the installer or
// running machine commands performs no terminal queries or color output.
func (w *setupWizard) choose(ctx context.Context, step, title, description string, options []wizardOption, value *string) (resultErr error) {
	if w.outputErr != nil {
		return w.outputErr
	}
	if w.plain {
		return w.plainChoose(ctx, step, title, description, options, value)
	}
	input, ok := w.config.In.(*os.File)
	if !ok {
		return errors.New("visual setup requires terminal input")
	}
	reader, err := cancelreader.NewReader(input)
	if err != nil {
		return errors.New("terminal input is unavailable")
	}
	stopInput := watchInput(ctx, reader)
	defer stopInput()
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return errors.New("terminal mode is unavailable")
	}
	defer func() {
		if err := term.Restore(int(input.Fd()), state); err != nil {
			resultErr = errors.Join(resultErr, errors.New("terminal mode could not be restored"))
		}
	}()
	w.print("%s", "\x1b[?25l")
	defer func() { w.print("%s", "\x1b[?25h"); resultErr = errors.Join(resultErr, w.outputErr) }()
	selected := 0
	for i, o := range options {
		if o.value == *value {
			selected = i
		}
	}
	lines := 0
	draw := func() {
		var b strings.Builder
		accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#B9A2FF"))
		body := lipgloss.NewStyle().Width(w.width() - 6)
		fmt.Fprintln(&b, accent.Render(step))
		fmt.Fprintln(&b, body.Bold(true).Render(title))
		fmt.Fprintln(&b, body.Foreground(lipgloss.Color("#A7B1C2")).Render(description))
		fmt.Fprintln(&b)
		start := max(0, selected-4)
		end := min(len(options), start+8)
		for i := start; i < end; i++ {
			prefix := "  "
			style := body
			if i == selected {
				prefix = "› "
				style = style.Foreground(lipgloss.Color("#6FE7CC")).Bold(true)
			}
			fmt.Fprintln(&b, style.Render(prefix+options[i].label))
		}
		fmt.Fprint(&b, "\n↑/↓ choose · Enter continue · Esc/Ctrl-C cancel")
		text := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#66758F")).Padding(1, 1).Width(w.width() - 2).Render(b.String())
		if lines > 0 {
			w.print("\x1b[%dA\r\x1b[J", lines)
		}
		w.print("%s", strings.ReplaceAll(text, "\n", "\r\n")+"\r\n")
		lines = strings.Count(text, "\n") + 1
	}
	draw()
	if w.outputErr != nil {
		return w.outputErr
	}
	one := make([]byte, 1)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err = reader.Read(one); err != nil {
			if ctx.Err() != nil || errors.Is(err, cancelreader.ErrCanceled) {
				return context.Canceled
			}
			return errors.New("terminal input ended before selection")
		}
		switch one[0] {
		case 3, 4:
			return context.Canceled
		case '\r', '\n':
			*value = options[selected].value
			return nil
		case 'k':
			selected = (selected + len(options) - 1) % len(options)
		case 'j', '\t':
			selected = (selected + 1) % len(options)
		case 27:
			// A bare Esc cancels immediately. Arrow keys arrive as one buffered terminal
			// sequence; read its continuation only when bytes are already available.
			ready := make(chan []byte, 1)
			go func() { buf := make([]byte, 2); n, _ := io.ReadFull(reader, buf); ready <- buf[:n] }()
			select {
			case seq := <-ready:
				if len(seq) == 2 && seq[0] == '[' {
					switch seq[1] {
					case 'A':
						selected = (selected + len(options) - 1) % len(options)
					case 'B':
						selected = (selected + 1) % len(options)
					default:
						return context.Canceled
					}
				} else {
					return context.Canceled
				}
			case <-ctx.Done():
				reader.Cancel()
				<-ready
				return context.Canceled
			case <-time.After(80 * time.Millisecond):
				reader.Cancel()
				<-ready
				return context.Canceled
			}
		default:
			continue
		}
		draw()
		if w.outputErr != nil {
			return w.outputErr
		}
	}
}

func (w *setupWizard) input(ctx context.Context, step, title, description string, value *string, secret bool) error {
	return w.plainInput(ctx, title, description, value, secret)
}

func (w *setupWizard) review(summary string) {
	if w.plain {
		w.print("%s\n", "\nReview before applying\n"+summary)
		return
	}
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#6FE7CC")).Padding(1, 1).Width(w.width() - 2)
	w.print("%s\n", style.Render("REVIEW YOUR SETUP\n\n"+summary))
}

func wizardSuccess(config Config, o options, report workflow.Object) string {
	message := "Setup complete. Start a fresh agent session to use these settings."
	if o.plain || config.Getenv("NO_COLOR") != "" || config.Getenv("TERM") == "dumb" {
		return message
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#6FE7CC")).Bold(true).Render("✓ " + message)
}
