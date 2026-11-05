package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

var (
	colorAccent = lipgloss.Color("#B9A2FF")
	colorMuted  = lipgloss.Color("#A7B1C2")
	colorDim    = lipgloss.Color("#798497")
	colorBorder = lipgloss.Color("#66758F")
	colorActive = lipgloss.Color("#6FE7CC")
	colorError  = lipgloss.Color("#FF9C9C")

	styleAccent = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleDim    = lipgloss.NewStyle().Foreground(colorDim)
	styleActive = lipgloss.NewStyle().Foreground(colorActive)
)

// escapeWait is how long a lone Esc waits for the rest of an arrow-key sequence.
const escapeWait = 50 * time.Millisecond

// visibleRows bounds how many menu rows one panel shows at a time.
const visibleRows = 10

func (w *setupWizard) size() (int, int) {
	width, height := 76, 40
	if f, ok := w.config.Err.(*os.File); ok {
		if cols, rows, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 && rows > 0 {
			width, height = min(width, cols-2), rows
		}
	}
	return max(width, 30), max(height, 8)
}

// enterScreen switches input to raw mode when it is a terminal and draws on the
// alternate screen with the cursor hidden. The returned function undoes both
// exactly once, so every exit path leaves the shell as it was.
func (w *setupWizard) enterScreen() (func() error, error) {
	restoreMode := func() error { return nil }
	if input, ok := w.config.In.(*os.File); ok && term.IsTerminal(int(input.Fd())) {
		state, err := term.MakeRaw(int(input.Fd()))
		if err != nil {
			return nil, errors.New("terminal mode is unavailable")
		}
		restoreMode = func() error {
			if err := term.Restore(int(input.Fd()), state); err != nil {
				return errors.New("terminal mode could not be restored")
			}
			return nil
		}
	}
	w.print("%s", "\x1b[?1049h\x1b[?25l")
	var once sync.Once
	var result error
	return func() error {
		once.Do(func() {
			w.print("%s", "\x1b[?25h\x1b[?1049l")
			result = restoreMode()
		})
		return result
	}, nil
}

type keyKind int

const (
	keyIgnored keyKind = iota
	keyText
	keyEnter
	keySpace
	keyBackspace
	keyDelete
	keyTab
	keyBacktab
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyEsc
	keyInterrupt
)

// keyPress is one decoded key. data holds the bytes of typed text, which the
// caller clears after use because it may be part of a secret.
type keyPress struct {
	kind keyKind
	data []byte
}

func (k keyPress) is(text string) bool { return k.kind == keyText && string(k.data) == text }

type readResult struct {
	b   byte
	ok  bool
	err error
}

// keyReader owns the only reader of terminal input during a visual setup. It
// reads one byte at a time so no input beyond the current key is buffered.
type keyReader struct {
	reader  cancelreader.CancelReader
	in      chan readResult
	quit    chan struct{}
	done    chan struct{}
	pending []byte
	err     error
}

func newKeyReader(reader cancelreader.CancelReader) *keyReader {
	k := &keyReader{reader: reader, in: make(chan readResult), quit: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(k.done)
		one := make([]byte, 1)
		for {
			n, err := reader.Read(one)
			r := readResult{err: err}
			if n == 1 {
				r.b, r.ok = one[0], true
				one[0] = 0
			}
			select {
			case k.in <- r:
			case <-k.quit:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return k
}

// close stops the reader goroutine. A reader that cannot be cancelled is left
// to finish its current read on its own.
func (k *keyReader) close() {
	close(k.quit)
	if k.reader.Cancel() {
		<-k.done
	}
	clear(k.pending)
}

// next returns the next byte. With wait > 0 it gives up after wait and reports
// ok == false, which is how a lone Esc is told apart from an escape sequence.
func (k *keyReader) next(ctx context.Context, wait time.Duration) (byte, bool, error) {
	if len(k.pending) > 0 {
		b := k.pending[0]
		k.pending = k.pending[1:]
		return b, true, nil
	}
	if k.err != nil {
		return 0, false, k.err
	}
	var timeout <-chan time.Time
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case r := <-k.in:
		if r.err != nil {
			k.err = r.err
		}
		if r.ok {
			return r.b, true, nil
		}
		return 0, false, k.err
	case <-ctx.Done():
		return 0, false, ctx.Err()
	case <-timeout:
		return 0, false, nil
	}
}

// pasteWait is how long a field waits for more already-sent input before it
// redraws; pasted text arrives well within it, typing does not.
const pasteWait = 5 * time.Millisecond

// textWaiting reports whether the next byte, arriving within pasteWait, is more
// printable text, so a paste is applied in full before the field is redrawn.
func (k *keyReader) textWaiting() bool {
	if len(k.pending) == 0 && k.err == nil {
		timer := time.NewTimer(pasteWait)
		defer timer.Stop()
		select {
		case r := <-k.in:
			if r.err != nil {
				k.err = r.err
			}
			if r.ok {
				k.pending = append(k.pending, r.b)
			}
		case <-timer.C:
		}
	}
	return len(k.pending) > 0 && k.pending[0] >= ' ' && k.pending[0] != 127
}

func inputError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, cancelreader.ErrCanceled) {
		return context.Canceled
	}
	if errors.Is(err, io.EOF) {
		return errors.New("terminal input ended before setup finished")
	}
	return err
}

func (k *keyReader) read(ctx context.Context) (keyPress, error) {
	b, _, err := k.next(ctx, 0)
	if err != nil {
		return keyPress{}, inputError(ctx, err)
	}
	switch b {
	case 3, 4:
		return keyPress{kind: keyInterrupt}, nil
	case '\r', '\n':
		return keyPress{kind: keyEnter}, nil
	case ' ':
		return keyPress{kind: keySpace}, nil
	case 8, 127:
		return keyPress{kind: keyBackspace}, nil
	case '\t':
		return keyPress{kind: keyTab}, nil
	case 27:
		return k.escape(ctx), nil
	}
	if b < 32 {
		return keyPress{kind: keyIgnored}, nil
	}
	data := []byte{b}
	need := 1
	switch {
	case b >= 0xF0:
		need = 4
	case b >= 0xE0:
		need = 3
	case b >= 0xC0:
		need = 2
	}
	for len(data) < need {
		c, ok, err := k.next(ctx, escapeWait)
		if err != nil || !ok {
			break
		}
		data = append(data, c)
	}
	return keyPress{kind: keyText, data: data}, nil
}

func (k *keyReader) escape(ctx context.Context) keyPress {
	b, ok, err := k.next(ctx, escapeWait)
	if err != nil || !ok {
		return keyPress{kind: keyEsc}
	}
	if b != '[' && b != 'O' {
		// Put the byte back in front so it is read as the next key.
		k.pending = append([]byte{b}, k.pending...)
		return keyPress{kind: keyEsc}
	}
	var params []byte
	for len(params) <= 8 {
		c, ok, err := k.next(ctx, escapeWait)
		if err != nil || !ok {
			return keyPress{kind: keyIgnored}
		}
		if c < 0x40 || c > 0x7e {
			params = append(params, c)
			continue
		}
		switch c {
		case 'A':
			return keyPress{kind: keyUp}
		case 'B':
			return keyPress{kind: keyDown}
		case 'C':
			return keyPress{kind: keyRight}
		case 'D':
			return keyPress{kind: keyLeft}
		case 'H':
			return keyPress{kind: keyHome}
		case 'F':
			return keyPress{kind: keyEnd}
		case 'Z':
			return keyPress{kind: keyBacktab}
		case '~':
			switch string(params) {
			case "1", "7":
				return keyPress{kind: keyHome}
			case "4", "8":
				return keyPress{kind: keyEnd}
			case "3":
				return keyPress{kind: keyDelete}
			case "5":
				return keyPress{kind: keyPageUp}
			case "6":
				return keyPress{kind: keyPageDown}
			}
		}
		return keyPress{kind: keyIgnored}
	}
	return keyPress{kind: keyIgnored}
}

// frame redraws the whole screen: title, stepper, answered steps and the
// active panel. Lines that do not fit are dropped from the top so the panel
// and its key hints stay visible.
func (w *setupWizard) frame(step int, panel string) {
	width, height := w.size()
	fit := lipgloss.NewStyle().MaxWidth(width)
	lines := []string{
		styleAccent.Render("◈ AGENT WORKFLOWS") + styleMuted.Render("  setup · "+w.view.Target.Version),
		w.stepper(step),
		"",
	}
	if done := w.summaries(step); len(done) > 0 {
		for _, s := range done {
			lines = append(lines, styleActive.Render("✓ ")+styleMuted.Render(padRight(s[0], 8))+s[1])
		}
		lines = append(lines, "")
	}
	lines = append(lines, strings.Split(panel, "\n")...)
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(fit.Render(line))
		b.WriteString("\x1b[K")
	}
	b.WriteString("\x1b[J")
	w.print("%s", b.String())
}

func (w *setupWizard) stepper(step int) string {
	parts := make([]string, 0, len(stepNames))
	for i, name := range stepNames {
		switch {
		case i < step:
			parts = append(parts, styleActive.Render("✓ "+name))
		case i == step:
			parts = append(parts, styleAccent.Render("● "+name))
		default:
			parts = append(parts, styleDim.Render("○ "+name))
		}
	}
	return strings.Join(parts, styleDim.Render(" ─ "))
}

// wrapHanging breaks a plain text line at spaces to fit width. Continuation
// lines start under the value: the column after the first run of two or more
// spaces that follows the label.
func wrapHanging(line string, width int) []string {
	if utf8.RuneCountInString(line) <= width || width < 20 {
		return []string{line}
	}
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if i := strings.Index(line[indent:], "  "); i > 0 {
		rest := line[indent+i:]
		indent += i + len(rest) - len(strings.TrimLeft(rest, " "))
	}
	if indent > width/2 {
		indent = 2
	}
	// Keep the line's own indent and inner spacing; a space run at a break
	// point is dropped.
	lead := len(line) - len(strings.TrimLeft(line, " "))
	words := strings.Split(line[lead:], " ")
	var out []string
	current := line[:lead] + words[0]
	for _, word := range words[1:] {
		switch {
		case utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) <= width:
			current += " " + word
		case word == "":
		default:
			out = append(out, strings.TrimRight(current, " "))
			current = strings.Repeat(" ", indent) + word
		}
	}
	return append(out, current)
}

func padRight(s string, n int) string {
	if gap := n - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func (w *setupWizard) panel(title, help string, rows []string, message, hints string) string {
	width, _ := w.size()
	inner := width - 4
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	if help != "" {
		b.WriteString("\n" + styleMuted.Width(inner).Render(help))
	}
	if len(rows) > 0 {
		fit := lipgloss.NewStyle().MaxWidth(inner)
		b.WriteString("\n")
		for _, row := range rows {
			b.WriteString("\n" + fit.Render(row))
		}
	}
	if message != "" {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(colorError).Width(inner).Render("! "+message))
	}
	b.WriteString("\n\n" + styleDim.Width(inner).Render(hints))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1).Width(width - 2).Render(b.String())
}

// fitPanel renders the menu, windowing its body when the panel alone is taller
// than the terminal so the rows and key hints stay on screen.
func (w *setupWizard) fitPanel(m *menu, cursor int) string {
	m.page = 0
	panel := w.menuPanel(m, cursor)
	_, height := w.size()
	if n := strings.Count(panel, "\n") + 1; n > height && m.body != "" {
		width, _ := w.size()
		body := 0
		for _, line := range strings.Split(m.body, "\n") {
			body += len(wrapHanging(line, width-4))
		}
		m.page = max(body-(n-height)-1, 1)
		panel = w.menuPanel(m, cursor)
	}
	return panel
}

func (w *setupWizard) menuPanel(m *menu, cursor int) string {
	var rows []string
	if m.body != "" {
		width, _ := w.size()
		var body []string
		for _, line := range strings.Split(m.body, "\n") {
			body = append(body, wrapHanging(line, width-4)...)
		}
		if m.page > 0 && m.page < len(body) {
			m.scroll = min(max(m.scroll, 0), len(body)-m.page)
			end := m.scroll + m.page
			rows = append(rows, body[m.scroll:end]...)
			rows = append(rows, styleDim.Render(fmt.Sprintf("  lines %d-%d of %d · PgUp/PgDn scroll", m.scroll+1, end, len(body))))
		} else {
			rows = append(rows, body...)
		}
		rows = append(rows, "")
	}
	boxOf := func(item menuItem) string {
		switch {
		case item.fixed:
			return "✓ "
		case item.checked != nil && *item.checked:
			return "[x] "
		case item.checked != nil:
			return "[ ] "
		}
		return ""
	}
	labelWidth := 0
	for _, item := range m.items {
		if !item.heading {
			labelWidth = max(labelWidth, lipgloss.Width(boxOf(item)+item.label))
		}
	}
	start, end := 0, len(m.items)
	if end > visibleRows {
		start = min(max(cursor-visibleRows/2, 0), len(m.items)-visibleRows)
		end = start + visibleRows
	}
	if start > 0 {
		rows = append(rows, styleDim.Render("  ↑ more"))
	}
	for i := start; i < end; i++ {
		item := m.items[i]
		if item.heading {
			row := ""
			if item.label != "" {
				row = "  " + styleMuted.Bold(true).Render(item.label)
				if item.detail != "" {
					row += "  " + styleDim.Render(item.detail)
				}
			}
			rows = append(rows, row)
			continue
		}
		mark := "  "
		if i == cursor {
			mark = "› "
		}
		label := padRight(boxOf(item)+item.label, labelWidth)
		switch {
		case item.fixed:
			label = styleDim.Render(mark + label)
		case i == cursor:
			label = styleActive.Bold(true).Render(mark + label)
		default:
			label = mark + label
		}
		if item.detail != "" {
			label += "  " + styleMuted.Render(item.detail)
		}
		rows = append(rows, label)
	}
	if end < len(m.items) {
		rows = append(rows, styleDim.Render("  ↓ more"))
	}
	hints := "↑↓ move · Enter select · 1-9 jump"
	if m.checklist {
		hints = "↑↓ move · Space toggle · Enter continue"
	} else if slices.ContainsFunc(m.items, func(item menuItem) bool { return item.checked != nil && item.selectable() }) {
		hints += " · Space toggle"
	}
	if m.back {
		hints += " · ← back"
	}
	hints += " · Esc quit"
	return w.panel(m.title, m.help, rows, m.message, hints)
}

func digitIndex(m *menu, k keyPress) (int, bool) {
	if k.kind != keyText || len(k.data) != 1 || k.data[0] < '1' || k.data[0] > '9' {
		return 0, false
	}
	rows := m.selectable()
	n := int(k.data[0] - '1')
	if n >= len(rows) {
		return 0, false
	}
	return rows[n], true
}

func (w *setupWizard) tuiPick(ctx context.Context, m *menu) (string, nav, error) {
	cursor := m.initial()
	for {
		w.frame(m.step, w.fitPanel(m, cursor))
		if w.outputErr != nil {
			return "", navQuit, w.outputErr
		}
		k, err := w.keys.read(ctx)
		if err != nil {
			return "", navQuit, err
		}
		m.message = ""
		if m.page > 0 && (k.kind == keyPageUp || k.kind == keyPageDown) {
			if k.kind == keyPageUp {
				m.scroll -= m.page
			} else {
				m.scroll += m.page
			}
			continue
		}
		if i, ok := digitIndex(m, k); ok {
			cursor = i
			m.toggle(i)
			continue
		}
		switch {
		case k.kind == keyInterrupt:
			return "", navQuit, nil
		case k.kind == keyEsc || k.is("q"):
			quit, err := w.confirmQuit(ctx, m.step)
			if err != nil {
				return "", navQuit, err
			}
			if quit {
				return "", navQuit, nil
			}
		case k.kind == keyUp || k.kind == keyBacktab || k.is("k"):
			cursor = m.move(cursor, -1)
		case k.kind == keyDown || k.kind == keyTab || k.is("j"):
			cursor = m.move(cursor, 1)
		case k.kind == keyHome:
			cursor = m.move(len(m.items)-1, 1)
		case k.kind == keyEnd:
			cursor = m.move(0, -1)
		case k.kind == keyLeft || k.kind == keyBackspace:
			if m.back {
				return "", navBack, nil
			}
		case k.kind == keySpace:
			m.toggle(cursor)
		case k.kind == keyEnter:
			if m.checklist {
				if m.check != nil {
					if msg := m.check(); msg != "" {
						m.message = msg
						continue
					}
				}
				return "", navNext, nil
			}
			if m.items[cursor].checked != nil {
				m.toggle(cursor)
				continue
			}
			return m.items[cursor].value, navNext, nil
		}
	}
}

// confirmQuit asks before leaving setup; Esc again or Enter on the default
// keeps going.
func (w *setupWizard) confirmQuit(ctx context.Context, step int) (bool, error) {
	quit := false
	for {
		rows := []string{"  Keep going", "  Quit setup"}
		selected := 0
		if quit {
			selected = 1
		}
		rows[selected] = styleActive.Bold(true).Render("› " + strings.TrimPrefix(rows[selected], "  "))
		w.frame(step, w.panel("Quit setup?", "Your current configuration stays as it is. You can run setup again any time.", rows, "", "↑↓ move · Enter select · Esc keep going"))
		if w.outputErr != nil {
			return false, w.outputErr
		}
		k, err := w.keys.read(ctx)
		if err != nil {
			return false, err
		}
		switch {
		case k.is("1"):
			quit = false
		case k.is("2"):
			quit = true
		case k.kind == keyInterrupt || k.is("y"):
			return true, nil
		case k.kind == keyEsc || k.kind == keyLeft || k.kind == keyBackspace || k.is("n"):
			return false, nil
		case k.kind == keyUp || k.kind == keyDown || k.kind == keyTab || k.kind == keyBacktab || k.is("j") || k.is("k"):
			quit = !quit
		case k.kind == keyEnter:
			return quit, nil
		}
	}
}

// appendBytes appends without leaving copies of earlier contents behind when
// the buffer grows, because the buffer may hold a secret.
func appendBytes(buf, data []byte) []byte {
	if len(buf)+len(data) > cap(buf) {
		grown := make([]byte, len(buf), 2*cap(buf)+len(data))
		copy(grown, buf)
		clear(buf[:cap(buf)])
		buf = grown
	}
	return append(buf, data...)
}

func (w *setupWizard) tuiEdit(ctx context.Context, s editSpec) (string, nav, error) {
	if s.secret {
		if input, ok := w.config.In.(*os.File); ok && !term.IsTerminal(int(input.Fd())) {
			return "", navQuit, errors.New("hidden key input requires a terminal")
		}
	}
	buf := appendBytes(make([]byte, 0, 128), []byte(s.value))
	defer func() { clear(buf[:cap(buf)]) }()
	message := ""
	hints := "Enter save · Esc back · Ctrl-C quit"
	redraw := true
	for {
		if redraw {
			field := string(buf) + "▏"
			if s.secret {
				field = strings.Repeat("•", min(utf8.RuneCount(buf), 40)) + "▏"
			}
			w.frame(s.step, w.panel(s.title, s.help, []string{styleActive.Render("› ") + field}, message, hints))
			if w.outputErr != nil {
				return "", navQuit, w.outputErr
			}
		}
		k, err := w.keys.read(ctx)
		if err != nil {
			return "", navQuit, err
		}
		message = ""
		redraw = true
		switch k.kind {
		case keyInterrupt:
			return "", navQuit, nil
		case keyEsc:
			return "", navBack, nil
		case keyEnter:
			if msg := s.validate(string(buf)); msg != "" {
				message = msg
				if s.secret {
					clear(buf)
					buf = buf[:0]
				}
				continue
			}
			return string(buf), navNext, nil
		case keyBackspace, keyDelete:
			if len(buf) > 0 {
				_, size := utf8.DecodeLastRune(buf)
				clear(buf[len(buf)-size:])
				buf = buf[:len(buf)-size]
			}
		case keySpace:
			buf = appendBytes(buf, []byte{' '})
		case keyText:
			if len(buf)+len(k.data) > 64<<10 {
				message = "That value is too long."
			} else {
				buf = appendBytes(buf, k.data)
			}
			clear(k.data)
		}
		// Pasted text that is already waiting is applied before drawing; the
		// field is drawn again before any other key, such as Enter, is handled.
		if k.kind == keyText || k.kind == keySpace {
			redraw = !w.keys.textWaiting()
		}
	}
}
