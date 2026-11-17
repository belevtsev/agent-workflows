package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
)

// A candidate shares the response's raw-text budget. Larger logs retain only the
// first and last three chunks; the original retained log remains the authority.
func compactLogCandidate(s *session, id string, budget int) ([]contextpack.Line, int, error) {
	f, err := compactLogFile(s, id)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	return collectLogCandidate("logs/"+id+".log", f, budget)
}

type logCandidate struct {
	name      string
	remaining int
	complete  bool
	lines     []contextpack.Line
	preview   []contextpack.Line
	total     int
	number    int
	offset    int
}

func (c *logCandidate) append(text string) {
	line := contextpack.Line{Path: c.name, Number: c.number, Offset: c.offset, Text: text}
	c.total++
	if len(c.preview) < 4 {
		c.preview = append(c.preview, line)
	} else {
		copy(c.preview[1:3], c.preview[2:4])
		c.preview[3] = line
	}
	c.remaining -= len(text)
	if c.remaining < 0 {
		c.complete, c.lines = false, nil
	}
	if c.complete {
		c.lines = append(c.lines, line)
	}
	c.number += strings.Count(text, "\n")
	if last := strings.LastIndexByte(text, '\n'); last >= 0 {
		c.offset = len(text) - last - 1
	} else {
		c.offset += len(text)
	}
}

// Stream normalized UTF-8 into the same 1024-byte chunks as collectLog, without
// holding the complete noisy log. Consecutive invalid bytes share one replacement.
func collectLogCandidate(name string, reader io.Reader, budget int) ([]contextpack.Line, int, error) {
	limited := &io.LimitedReader{R: reader, N: contextpack.MaxArtifact + 1}
	input := bufio.NewReader(limited)
	c := logCandidate{name: name, remaining: budget, complete: budget > 0, number: 1}
	var chunk strings.Builder
	invalid := false
	for {
		r, size, err := input.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		bad := r == utf8.RuneError && size == 1
		if bad && invalid {
			continue
		}
		invalid = bad
		if chunk.Len()+utf8.RuneLen(r) > 1024 {
			c.append(chunk.String())
			chunk.Reset()
		}
		chunk.WriteRune(r)
	}
	if limited.N == 0 {
		return nil, 0, fmt.Errorf("log exceeds artifact size limit: %s", name)
	}
	if chunk.Len() > 0 {
		c.append(chunk.String())
	}
	if c.complete {
		return c.lines, c.total, nil
	}
	if c.total > 4 {
		c.preview = append(c.preview[1:4:4], c.preview[0])
	}
	return c.preview, c.total, nil
}

func compactLogBytes(lines []contextpack.Line) int {
	total := 0
	for _, line := range lines {
		total += len(line.Text)
	}
	return total
}
