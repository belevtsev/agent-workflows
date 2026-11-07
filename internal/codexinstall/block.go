package codexinstall

import (
	"bytes"
	"errors"
)

// StartMarker and EndMarker delimit the managed block in a host instruction
// file. Everything between them belongs to agent-workflows and is replaced on
// every install; everything outside them belongs to the user.
const (
	StartMarker = "<!-- agent-workflows-start -->"
	EndMarker   = "<!-- agent-workflows-end -->"
)

// ErrMalformedBlock reports marker lines that do not form exactly one
// well-formed pair, so the owned region cannot be located safely.
var ErrMalformedBlock = errors.New("instruction file must contain at most one well-formed agent-workflows marker pair")

// managedBlock wraps body in the marker lines, adding a final newline to body
// when it lacks one.
func managedBlock(body []byte) []byte {
	out := append([]byte(StartMarker+"\n"), body...)
	if len(body) > 0 && !bytes.HasSuffix(body, []byte("\n")) {
		out = append(out, '\n')
	}
	return append(out, EndMarker+"\n"...)
}

// locateBlock returns the byte range of the managed block, from the start
// marker through the end marker's newline. found is false when neither marker
// appears; any other marker arrangement is ErrMalformedBlock.
func locateBlock(data []byte) (start, end int, found bool, err error) {
	starts, ends := bytes.Count(data, []byte(StartMarker)), bytes.Count(data, []byte(EndMarker))
	if starts == 0 && ends == 0 {
		return 0, 0, false, nil
	}
	if starts != 1 || ends != 1 {
		return 0, 0, false, ErrMalformedBlock
	}
	start = bytes.Index(data, []byte(StartMarker))
	endMarker := bytes.Index(data, []byte(EndMarker))
	afterStart, tail := start+len(StartMarker), endMarker+len(EndMarker)
	if endMarker < afterStart ||
		(start > 0 && data[start-1] != '\n') || data[afterStart] != '\n' ||
		data[endMarker-1] != '\n' || tail >= len(data) || data[tail] != '\n' {
		return 0, 0, false, ErrMalformedBlock
	}
	return start, tail + 1, true, nil
}

// UpsertBlock returns data with the managed block for body inserted or
// replaced. Without markers the block is appended after one blank line (and a
// final newline when data lacks one); with a well-formed pair only the block's
// own bytes change. status is "inserted", "replaced" or "unchanged".
func UpsertBlock(data, body []byte) (out []byte, status string, err error) {
	block := managedBlock(body)
	start, end, found, err := locateBlock(data)
	if err != nil {
		return nil, "", err
	}
	if found {
		if bytes.Equal(data[start:end], block) {
			return bytes.Clone(data), "unchanged", nil
		}
		out = append(bytes.Clone(data[:start]), block...)
		return append(out, data[end:]...), "replaced", nil
	}
	out = bytes.Clone(data)
	if len(out) > 0 {
		if !bytes.HasSuffix(out, []byte("\n")) {
			out = append(out, '\n')
		}
		out = append(out, '\n')
	}
	return append(out, block...), "inserted", nil
}

// RemoveBlock returns data without the managed block. When the block ends the
// file, the blank separator line UpsertBlock added before it goes too, so
// removing an inserted block restores data that was empty or ended with a
// newline exactly.
func RemoveBlock(data []byte) (out []byte, removed bool, err error) {
	start, end, found, err := locateBlock(data)
	if err != nil || !found {
		return bytes.Clone(data), false, err
	}
	before := data[:start]
	if end == len(data) && bytes.HasSuffix(before, []byte("\n\n")) {
		before = before[:len(before)-1]
	}
	out = append(bytes.Clone(before), data[end:]...)
	return out, true, nil
}
