package onboarding

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
)

const workflowEventLimit = 4 << 20

// workflowStream receives stdout from one proc.Run writer goroutine. The
// supervisor reads the decoder's result only after proc.Run drained stdout.
type workflowStream struct {
	line   []byte
	parse  func([]byte) error
	cancel context.CancelFunc
	err    error
}

func (s *workflowStream) Write(p []byte) (int, error) {
	size := len(p)
	for len(p) > 0 && s.err == nil {
		prefix, suffix, complete := bytes.Cut(p, []byte{'\n'})
		if len(prefix) > workflowEventLimit-len(s.line) {
			s.fail(errors.New("host event exceeded its size limit"))
			break
		}
		s.line = append(s.line, prefix...)
		if complete {
			s.consume()
		}
		p = suffix
	}
	return size, nil
}

func (s *workflowStream) consume() {
	line := bytes.TrimSpace(s.line)
	if len(line) != 0 && s.err == nil {
		if err := s.parse(line); err != nil {
			s.fail(err)
		}
	}
	s.line = s.line[:0]
}

func (s *workflowStream) fail(err error) {
	s.err = err
	s.cancel()
}

func (s *workflowStream) finish() error {
	s.consume()
	return s.err
}

func readWorkflowPayload(scratch, filename string) ([]byte, error) {
	root, err := os.OpenRoot(scratch)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > workflowResultLimit {
		return nil, errors.New("host did not write a bounded regular result file")
	}
	file, err := root.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("host result file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, workflowResultLimit+1))
	if err != nil || !workflowJSONObject(data, workflowResultLimit) {
		return nil, errors.New("host result is not a bounded JSON object")
	}
	return bytes.TrimSpace(data), nil
}
