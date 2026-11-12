package consult

import (
	"errors"
	"testing"
)

// fxWriteCloser records what writeRecord does to the reserved output.
type fxWriteCloser struct {
	writeErr, closeErr error
	written            []byte
	closed             int
}

func (f *fxWriteCloser) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.written = append(f.written, p...)
	return len(p), nil
}

func (f *fxWriteCloser) Close() error {
	f.closed++
	return f.closeErr
}

// oracle: writeRecord closes the reserved output exactly once and reports a
// failed Close as a failed write; a failed Write wins over a failed Close.
func TestWriteRecordReportsCloseErrors(t *testing.T) {
	t.Parallel()
	writeErr, closeErr := errors.New("write failed"), errors.New("close failed")
	cases := []struct {
		name string
		out  fxWriteCloser
		want error
	}{
		{"success", fxWriteCloser{}, nil},
		{"close fails", fxWriteCloser{closeErr: closeErr}, closeErr},
		{"write fails", fxWriteCloser{writeErr: writeErr}, writeErr},
		{"both fail", fxWriteCloser{writeErr: writeErr, closeErr: closeErr}, writeErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := tc.out
			err := writeRecord(&out, newObject("version", number(recordVersion)))
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
			if out.closed != 1 {
				t.Fatalf("closed %d times, want 1", out.closed)
			}
			if tc.out.writeErr == nil && len(out.written) == 0 {
				t.Fatal("record not written")
			}
		})
	}
}
