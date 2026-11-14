package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// Requirement "Log limit" (openspec/changes/reduce-cw-to-evidence-gate/specs/
// check-evidence/spec.md): "`cw` SHALL keep at most 10 MiB of a run's output,
// the first 1 MiB and the last 9 MiB, and SHALL mark in the log where output
// was dropped."
//
// These tests write a stream in which every position can be recognised (each
// 16-byte line holds its own offset) and check the log by content, byte for
// byte, for several ways of cutting the stream into writes.

// lineWidth is the width of one line of a numbered stream.
const lineWidth = 16

// numbered is a stream of size bytes made of 16-byte lines, each holding its
// own offset: "000000000000000\n", "000000000000016\n", ... The last line is
// cut when size is not a multiple of 16. A shorter stream is a prefix of a
// longer one.
func numbered(size int) []byte {
	stream := make([]byte, (size+lineWidth-1)/lineWidth*lineWidth)
	for offset := 0; offset < len(stream); offset += lineWidth {
		line := stream[offset : offset+lineWidth]
		line[lineWidth-1] = '\n'
		for i, n := lineWidth-2, offset; i >= 0; i, n = i-1, n/10 {
			line[i] = byte('0' + n%10)
		}
	}
	return stream[:size]
}

// writePlan says how many bytes the next write takes, at offset of the
// stream.
type writePlan func(offset int) int

// fixed writes n bytes at a time.
func fixed(n int) writePlan { return func(int) int { return n } }

// singleNear writes one byte at a time within a window around each of the
// given offsets and big bytes elsewhere, so a stream of tens of MiB is cut
// into single bytes where it matters without millions of writes.
func singleNear(big, window int, offsets ...int) writePlan {
	return func(offset int) int {
		next := big
		for _, at := range offsets {
			switch {
			case offset >= at-window && offset < at+window:
				return 1
			case offset < at-window:
				next = min(next, at-window-offset)
			}
		}
		return next
	}
}

// writeLog writes stream to a new log in the writes plan gives, closes it and
// returns what the file holds.
func writeLog(t *testing.T, stream []byte, plan writePlan) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	log := newLog(file)
	for offset := 0; offset < len(stream); {
		n := min(max(plan(offset), 1), len(stream)-offset)
		written, err := log.Write(stream[offset : offset+n])
		if err != nil || written != n {
			t.Fatalf("Write of %d bytes at offset %d: %d, %v", n, offset, written, err)
		}
		offset += n
	}
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// marker matches the line the log holds where output was dropped, and
// captures the number of bytes it says were dropped.
var marker = regexp.MustCompile(`\n--- cw: (\d+) bytes of output were dropped here ---\n`)

// describe names the part of a numbered stream that data starts with, for a
// failure message.
func describe(data []byte) string {
	return strconv.Quote(string(data[:min(len(data), 2*lineWidth)]))
}

// checkLog checks the content of a log made from stream.
func checkLog(t *testing.T, stream, data []byte) {
	t.Helper()
	const limit = headLimit + tailLimit
	if len(stream) <= limit {
		if !bytes.Equal(data, stream) {
			t.Errorf("a stream of %d bytes, at most the limit of %d, was not kept whole: the log holds %d bytes, starting %s",
				len(stream), limit, len(data), describe(data))
		}
		if marker.Match(data) {
			t.Errorf("a stream of %d bytes, at most the limit, has a marker of dropped output", len(stream))
		}
		return
	}
	marks := marker.FindAllSubmatchIndex(data, -1)
	if len(marks) != 1 {
		t.Fatalf("a stream of %d bytes: the log of %d bytes holds %d markers, want 1", len(stream), len(data), len(marks))
	}
	mark := marks[0]
	head, tail := data[:mark[0]], data[mark[1]:]
	if !bytes.Equal(head, stream[:headLimit]) {
		t.Errorf("the head is %d bytes starting %s, want the first %d bytes of the stream, starting %s",
			len(head), describe(head), headLimit, describe(stream))
	}
	if !bytes.Equal(tail, stream[len(stream)-tailLimit:]) {
		want := stream[len(stream)-tailLimit:]
		first := 0
		for first < min(len(tail), len(want)) && tail[first] == want[first] {
			first++
		}
		t.Errorf("the tail is %d bytes starting %s, want the last %d bytes of the stream, starting %s; they first differ at byte %d of the tail",
			len(tail), describe(tail), tailLimit, describe(want), first)
	}
	dropped, err := strconv.Atoi(string(data[mark[2]:mark[3]]))
	if want := len(stream) - limit; err != nil || dropped != want {
		t.Errorf("the marker says %q bytes were dropped, want %d", data[mark[2]:mark[3]], want)
	}
}

func TestLogKeepsTheHeadAMarkerAndTheTail(t *testing.T) {
	const limit = headLimit + tailLimit
	far := limit + 7*tailLimit/3 + 5 // the tail ring turns more than twice, and the stream ends inside a line
	cases := []struct {
		name string
		size int
		plan writePlan
	}{
		{"empty", 0, fixed(1)},
		{"one byte at a time, far below the limit", 20000, fixed(1)},
		{"a few hundred bytes at a time, below the limit", headLimit + 12345, fixed(333)},
		{"a few hundred bytes at a time, exactly at the limit", limit, fixed(333)},
		{"one write, exactly at the limit", limit, fixed(limit)},
		{"a few hundred bytes at a time, one byte over the limit", limit + 1, fixed(333)},
		{"a few hundred bytes at a time, far above the limit", far, fixed(333)},
		{"writes larger than the tail, far above the limit", far, fixed(tailLimit + 12345)},
		{"one write of the whole stream, far above the limit", far, fixed(far)},
		{"the first write ends exactly at the head", far, func(offset int) int {
			if offset == 0 {
				return headLimit
			}
			return 65536
		}},
		{"one write ends exactly at the limit", far, func(offset int) int {
			if offset < limit {
				return min(1<<20, limit-offset)
			}
			return 4093
		}},
		{"one write spans the head and the whole tail", far, func(offset int) int {
			if offset == 0 {
				return headLimit - 7
			}
			return tailLimit + 100
		}},
		{"one byte at a time around the head, the limit and where the kept tail starts", far,
			singleNear(1<<20, 600, headLimit, limit, far-tailLimit)},
	}
	for _, c := range cases {
		// Not parallel: each case holds tens of MiB.
		t.Run(c.name, func(t *testing.T) {
			stream := numbered(c.size)
			checkLog(t, stream, writeLog(t, stream, c.plan))
		})
	}
}

// A run's own last line, written after the program's output, is part of the
// stream like any other: it ends the tail.
func TestLogEndsWithTheLastWrite(t *testing.T) {
	t.Parallel()
	stream := append(numbered(headLimit+tailLimit+1000), "\ncw: the check did not end within its timeout\n"...)
	data := writeLog(t, stream, fixed(1<<16))
	checkLog(t, stream, data)
	if !bytes.HasSuffix(data, []byte("\ncw: the check did not end within its timeout\n")) {
		t.Errorf("the log does not end with the last write; it ends %q", data[max(0, len(data)-64):])
	}
}
