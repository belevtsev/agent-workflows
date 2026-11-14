package usagelog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"

	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// lenient decoding tolerates transcript quirks that do not affect usage.
var lenient = json.JoinOptions(jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))

// reader streams JSONL files under one shared byte budget, skipping (and
// counting) lines longer than the buffer.
type reader struct {
	buffer    *bufio.Reader
	remaining int64
	exhausted bool
}

func newReader(lineLimit int) *reader {
	return &reader{buffer: bufio.NewReaderSize(nil, lineLimit), remaining: MaxTotalBytes}
}

type fileStats struct {
	bytes, lines int64
	complete     bool
}

// eachLine calls visit for every nonblank line of path. It stops early when
// the shared budget is exhausted (complete=false) or ctx is cancelled.
func (r *reader) eachLine(ctx context.Context, path string, skipped *Skipped, visit func([]byte)) (fileStats, error) {
	stats := fileStats{}
	file, err := os.Open(path)
	if err != nil {
		return stats, err
	}
	defer file.Close()
	r.buffer.Reset(file)
	for {
		if stats.lines%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return stats, err
			}
		}
		if r.remaining <= 0 {
			r.exhausted = true
			return stats, nil
		}
		data, err := r.buffer.ReadSlice('\n')
		stats.bytes += int64(len(data))
		r.remaining -= int64(len(data))
		if errors.Is(err, bufio.ErrBufferFull) {
			skipped.OversizedLines++
			stats.lines++
			for errors.Is(err, bufio.ErrBufferFull) {
				if r.remaining <= 0 {
					r.exhausted = true
					return stats, nil
				}
				data, err = r.buffer.ReadSlice('\n')
				stats.bytes += int64(len(data))
				r.remaining -= int64(len(data))
			}
			if err == io.EOF {
				stats.complete = true
				return stats, nil
			}
			if err != nil {
				return stats, err
			}
			continue
		}
		if err != nil && err != io.EOF {
			return stats, err
		}
		if line := bytes.TrimSpace(data); len(line) > 0 {
			stats.lines++
			visit(line)
		}
		if err == io.EOF {
			stats.complete = true
			return stats, nil
		}
	}
}

// firstLine returns the first line of path, bounded by limit.
func firstLine(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buffer := bufio.NewReaderSize(file, min(limit, 64<<10))
	var line []byte
	for {
		chunk, err := buffer.ReadSlice('\n')
		if len(line)+len(chunk) > limit {
			return nil, errors.New("first line exceeds the scan limit")
		}
		line = append(line, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		return bytes.TrimSpace(line), nil
	}
}

type envelope struct {
	Type string `json:"type"`
}

// decodeType validates a whole line as a JSON object and returns its type.
func decodeType(line []byte) (string, bool) {
	var e envelope
	if err := json.Unmarshal(line, &e, lenient); err != nil {
		return "", false
	}
	return e.Type, true
}
