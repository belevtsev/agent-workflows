package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// plainPick shows a numbered menu. Enter keeps the marked default, b goes back
// and q quits; checklists toggle the listed numbers until Enter continues.
func (w *setupWizard) plainPick(ctx context.Context, m *menu) (string, nav, error) {
	for {
		if m.title == stepNames[m.step] {
			w.print("\n%s\n", m.title)
		} else {
			w.print("\n%s · %s\n", stepNames[m.step], m.title)
		}
		if m.help != "" {
			w.print("%s\n", m.help)
		}
		if m.body != "" {
			w.print("\n%s\n\n", m.body)
		}
		rows := m.selectable()
		def := m.initial()
		number := 0
		for i, item := range m.items {
			detail := ""
			if item.detail != "" {
				detail = "  (" + item.detail + ")"
			}
			if item.heading {
				if item.label == "" {
					w.print("\n")
				} else {
					w.print("   %s%s\n", item.label, detail)
				}
				continue
			}
			if item.fixed {
				w.print("     - %s%s\n", item.label, detail)
				continue
			}
			number++
			mark := " "
			if !m.checklist && i == def {
				mark = "*"
			}
			box := ""
			if item.checked != nil {
				box = "[ ] "
				if *item.checked {
					box = "[x] "
				}
			}
			w.print(" %s %d  %s%s%s\n", mark, number, box, item.label, detail)
		}
		if m.message != "" {
			w.print("%s\n", m.message)
			m.message = ""
		}
		back := ""
		if m.back {
			back = ", b back"
		}
		if m.checklist {
			w.print("Numbers to toggle, Enter to continue%s, q quit: ", back)
		} else {
			w.print("Choice [%d]%s, q quit: ", slicesIndex(rows, def)+1, back)
		}
		if w.outputErr != nil {
			return "", navQuit, w.outputErr
		}
		line, err := w.line(ctx)
		if err != nil {
			return "", navQuit, err
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q":
			return "", navQuit, nil
		case "b":
			if m.back {
				return "", navBack, nil
			}
			m.message = "This is the first step."
			continue
		}
		if m.checklist {
			if line == "" {
				if m.check != nil {
					if msg := m.check(); msg != "" {
						m.message = msg
						continue
					}
				}
				return "", navNext, nil
			}
			// Toggle only when every entry is a listed number.
			var picks []int
			for _, field := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ' ' }) {
				n, err := strconv.Atoi(field)
				if err != nil || n < 1 || n > len(rows) {
					m.message = "Enter listed numbers, or press Enter to continue."
					picks = nil
					break
				}
				picks = append(picks, rows[n-1])
			}
			for _, i := range picks {
				m.toggle(i)
			}
			continue
		}
		idx := def
		if line != "" {
			n, err := strconv.Atoi(line)
			if err != nil || n < 1 || n > len(rows) {
				m.message = "Enter a listed number, b to go back or q to quit."
				continue
			}
			idx = rows[n-1]
		}
		m.value = m.items[idx].value
		if m.items[idx].checked != nil {
			m.toggle(idx)
			continue
		}
		return m.items[idx].value, navNext, nil
	}
}

func slicesIndex(rows []int, v int) int {
	for i, r := range rows {
		if r == v {
			return i
		}
	}
	return 0
}

// plainEdit reads one value. A hidden secret returns "" for an empty entry;
// a visible value keeps its current text on Enter and "default" clears it.
func (w *setupWizard) plainEdit(ctx context.Context, s editSpec) (string, nav, error) {
	for {
		w.print("\n%s\n%s\n", s.title, s.help)
		var value string
		if s.secret {
			// A file that is not a terminal would echo or log the key; any
			// other reader has no echo to turn off.
			input, ok := w.config.In.(*os.File)
			if ok && !term.IsTerminal(int(input.Fd())) {
				return "", navQuit, errors.New("hidden key input requires a terminal")
			}
			w.print("%s", "Key (hidden, Enter keeps the current key): ")
			if w.outputErr != nil {
				return "", navQuit, w.outputErr
			}
			data, err := w.secret(ctx, input)
			w.print("\n")
			if err != nil {
				clear(data)
				return "", navQuit, err
			}
			value = string(data)
			clear(data)
			if value == "b" {
				// The plain interface reserves b for back; no key is one letter.
				return "", navBack, nil
			}
		} else {
			w.print("Value [%s] (Enter keeps it, default follows the release, b goes back): ", s.value)
			if w.outputErr != nil {
				return "", navQuit, w.outputErr
			}
			line, err := w.line(ctx)
			if err != nil {
				return "", navQuit, err
			}
			switch line = strings.TrimSpace(line); line {
			case "b":
				return "", navBack, nil
			case "":
				value = s.value
			case "default":
				value = ""
			default:
				value = line
			}
		}
		if msg := s.validate(value); msg != "" {
			w.print("%s\n", msg)
			continue
		}
		return value, navNext, nil
	}
}

// Read a single line without buffering subsequent pasted secret input.
func (w *setupWizard) line(ctx context.Context) (text string, resultErr error) {
	raw := false
	if input, ok := w.config.In.(*os.File); ok && term.IsTerminal(int(input.Fd())) {
		state, err := term.MakeRaw(int(input.Fd()))
		if err != nil {
			return "", errors.New("terminal input mode is unavailable")
		}
		raw = true
		defer func() {
			if err := term.Restore(int(input.Fd()), state); err != nil {
				resultErr = errors.Join(resultErr, errors.New("terminal mode could not be restored"))
			}
		}()
	}
	var value []byte
	one := make([]byte, 1)
	var reader io.Reader = w.config.In
	if w.reader != nil {
		reader = w.reader
	}
	for len(value) < 65536 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := reader.Read(one)
		if n == 1 {
			switch one[0] {
			case 3, 4, 27:
				return "", context.Canceled
			case '\r', '\n':
				if raw {
					w.print("\r\n")
				}
				return strings.TrimSuffix(string(value), "\r"), w.outputErr
			case 8, 127:
				if len(value) > 0 {
					_, size := utf8.DecodeLastRune(value)
					value = value[:len(value)-size]
					if raw {
						w.print("\b \b")
					}
				}
			default:
				value = append(value, one[0])
				if raw && one[0] >= 32 {
					w.print("%s", string(one))
				}
			}
		}
		if w.outputErr != nil {
			return "", w.outputErr
		}
		if err != nil {
			if errors.Is(err, cancelreader.ErrCanceled) || ctx.Err() != nil {
				return "", context.Canceled
			}
			if errors.Is(err, io.EOF) {
				return "", errors.New("terminal input ended before setup finished")
			}
			return "", err
		}
	}
	return "", errors.New("terminal input exceeds limit")
}

// secret reads a hidden line from w.reader. When input is a terminal it is
// switched to raw mode so nothing is echoed; input is nil for a non-file reader.
func (w *setupWizard) secret(ctx context.Context, input *os.File) (_ []byte, resultErr error) {
	reader := w.reader
	if reader == nil {
		return nil, errors.New("hidden terminal input is unavailable")
	}
	if input != nil {
		state, err := term.MakeRaw(int(input.Fd()))
		if err != nil {
			return nil, errors.New("hidden terminal input is unavailable")
		}
		defer func() {
			if err := term.Restore(int(input.Fd()), state); err != nil {
				resultErr = errors.Join(resultErr, errors.New("terminal mode could not be restored"))
			}
		}()
	}
	value := make([]byte, 0, 128)
	one := make([]byte, 1)
	for len(value) <= 64<<10 {
		if err := ctx.Err(); err != nil {
			clear(value[:cap(value)])
			return nil, err
		}
		if _, err := reader.Read(one); err != nil {
			clear(value[:cap(value)])
			switch {
			case errors.Is(err, cancelreader.ErrCanceled) || ctx.Err() != nil:
				return nil, context.Canceled
			case errors.Is(err, io.EOF):
				return nil, errors.New("terminal input ended before setup finished")
			}
			return nil, err
		}
		switch one[0] {
		case 3, 4, 27:
			clear(value[:cap(value)])
			return nil, context.Canceled
		case '\r', '\n':
			one[0] = 0
			return value, nil
		case 8, 127:
			if len(value) > 0 {
				_, size := utf8.DecodeLastRune(value)
				clear(value[len(value)-size:])
				value = value[:len(value)-size]
			}
		default:
			value = appendBytes(value, one)
		}
		one[0] = 0
	}
	clear(value[:cap(value)])
	return nil, errors.New("API key exceeds input limit")
}
