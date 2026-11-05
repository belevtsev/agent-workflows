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

func (w *setupWizard) plainChoose(ctx context.Context, step, title, description string, options []wizardOption, value *string) error {
	w.print("\n%s · %s\n%s\n", step, title, description)
	selected := 0
	for i, o := range options {
		mark := " "
		if o.value == *value {
			mark = "*"
			selected = i
		}
		w.print(" %s %d  %s\n", mark, i+1, o.label)
	}
	w.print("Choice [%d]: ", selected+1)
	if w.outputErr != nil {
		return w.outputErr
	}
	line, err := w.line(ctx)
	if err != nil {
		return err
	}
	if line == "" {
		return nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(options) {
		return errors.New("select a listed choice number")
	}
	*value = options[n-1].value
	return nil
}

func (w *setupWizard) plainInput(ctx context.Context, title, description string, value *string, secret bool) error {
	w.print("\n%s\n%s\n", title, description)
	if secret {
		input, ok := w.config.In.(*os.File)
		if !ok || !term.IsTerminal(int(input.Fd())) {
			return errors.New("hidden key input requires a terminal")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		w.print("%s", "Key (hidden): ")
		if w.outputErr != nil {
			return w.outputErr
		}
		data, err := w.secret(ctx, input)
		defer clear(data)
		w.print("\n")
		if err != nil {
			return err
		}
		*value = string(data)
		clear(data)
		return ctx.Err()
	}
	w.print("Value [%s] (type default to inherit): ", *value)
	if w.outputErr != nil {
		return w.outputErr
	}
	line, err := w.line(ctx)
	if err == nil && line != "" {
		if line == "default" {
			*value = ""
		} else {
			*value = line
		}
	}
	return err
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
			if errors.Is(err, io.EOF) || errors.Is(err, cancelreader.ErrCanceled) || ctx.Err() != nil {
				return "", context.Canceled
			}
			return "", err
		}
	}
	return "", errors.New("terminal input exceeds limit")
}

func (w *setupWizard) secret(ctx context.Context, input *os.File) (_ []byte, resultErr error) {
	reader := w.reader
	if reader == nil {
		var err error
		reader, err = cancelreader.NewReader(input)
		if err != nil {
			return nil, errors.New("hidden terminal input is unavailable")
		}
		stopInput := watchInput(ctx, reader)
		defer stopInput()
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return nil, errors.New("hidden terminal input is unavailable")
	}
	defer func() {
		if err := term.Restore(int(input.Fd()), state); err != nil {
			resultErr = errors.Join(resultErr, errors.New("terminal mode could not be restored"))
		}
	}()
	var value []byte
	one := make([]byte, 1)
	for len(value) <= 64<<10 {
		if err := ctx.Err(); err != nil {
			clear(value)
			return nil, err
		}
		if _, err = reader.Read(one); err != nil {
			clear(value)
			return nil, context.Canceled
		}
		switch one[0] {
		case 3, 4, 27:
			clear(value)
			return nil, context.Canceled
		case '\r', '\n':
			return value, nil
		case 8, 127:
			if len(value) > 0 {
				_, size := utf8.DecodeLastRune(value)
				clear(value[len(value)-size:])
				value = value[:len(value)-size]
			}
		default:
			value = append(value, one[0])
		}
	}
	clear(value)
	return nil, errors.New("API key exceeds input limit")
}
