package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// markerQuery returns the capture marker's HTML once the injected script has
// reported, or an empty string while the export is still running.
const markerQuery = `(function () {
  var element = document.getElementById('cwdev-svg-export');
  return element ? element.outerHTML : '';
})()`

// devtoolsPollInterval is how often the page is asked for the marker.
const devtoolsPollInterval = 100 * time.Millisecond

// captureWithChrome opens page in headless Chrome, driven over the DevTools
// protocol on a pipe (file descriptors 3 and 4, as archify's own browser
// checks do), and returns the capture marker the injected script appends.
// Unlike --dump-dom with a virtual time budget, a real page runs animation
// frames, so the viewer's frame-scheduled layout settles before the export.
func captureWithChrome(ctx context.Context, chrome string, args []string) ([]byte, error) {
	// Chrome reads commands from descriptor 3 and writes replies to 4.
	commandsRead, commandsWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	repliesRead, repliesWrite, err := os.Pipe()
	if err != nil {
		commandsRead.Close()
		commandsWrite.Close()
		return nil, err
	}
	browser := exec.Command(chrome, args...)
	browser.ExtraFiles = []*os.File{commandsRead, repliesWrite}
	var stderr lockedTail
	browser.Stderr = &stderr
	// A helper that outlives the browser and keeps stderr open must not hold
	// Wait forever, and the browser's helpers are stopped with it.
	browser.WaitDelay = 2 * time.Second
	ownProcessGroup(browser)
	startErr := browser.Start()
	commandsRead.Close()
	repliesWrite.Close()
	if startErr != nil {
		commandsWrite.Close()
		repliesRead.Close()
		return nil, fmt.Errorf("start headless chrome: %w", startErr)
	}
	exited := make(chan error, 1)
	go func() { exited <- browser.Wait() }()

	session := newDevtools(commandsWrite, repliesRead)
	marker, captureErr := session.capture(ctx)

	// Ask Chrome to close, then make sure it is gone. After a timeout or an
	// interrupt there is nothing to wait for.
	grace := 5 * time.Second
	if ctx.Err() != nil {
		_ = killProcessGroup(browser)
		grace = 0
	} else {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = session.call(closeCtx, "", "Browser.close", nil)
		cancel()
	}
	commandsWrite.Close()
	var waitErr error
	select {
	case waitErr = <-exited:
	case <-time.After(grace):
		_ = killProcessGroup(browser)
		waitErr = <-exited
	}
	// Stop helpers that outlived the browser process, and let them exit
	// before the caller removes their profile directory.
	_ = killProcessGroup(browser)
	waitProcessGroupGone(browser, processGroupGrace)
	repliesRead.Close()
	if captureErr != nil {
		detail := stderr.String()
		if ctx.Err() != nil {
			// Keep the cause the deadline cut short, such as a crashed page
			// that kept answering with errors.
			if detail == "" {
				return nil, captureErr
			}
			return nil, fmt.Errorf("%w\nchrome stderr: %s", captureErr, detail)
		}
		if waitErr != nil {
			return nil, fmt.Errorf("headless chrome failed: %v (%w)\n%s", captureErr, waitErr, detail)
		}
		return nil, fmt.Errorf("headless chrome failed: %w\n%s", captureErr, detail)
	}
	return marker, nil
}

// capture attaches to the page and polls it for the capture marker.
func (session *devtools) capture(ctx context.Context) ([]byte, error) {
	target, err := session.pageTarget(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := session.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})
	if err != nil {
		return nil, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(reply, &attached); err != nil || attached.SessionID == "" {
		return nil, fmt.Errorf("attach to page: unexpected reply %s", reply)
	}
	var lastErr error
	for {
		reply, err := session.call(ctx, attached.SessionID, "Runtime.evaluate", map[string]any{
			"expression": markerQuery, "returnByValue": true,
		})
		if err != nil && !errors.Is(err, errDevtoolsCommand) {
			if lastErr != nil && ctx.Err() != nil {
				return nil, fmt.Errorf("%w (last page error: %v)", err, lastErr)
			}
			return nil, err
		}
		if err != nil {
			lastErr = err
		}
		// A command error (for example a context destroyed by the initial
		// navigation) is retried until the deadline.
		if err == nil {
			var evaluated struct {
				Result struct {
					Value any `json:"value"`
				} `json:"result"`
			}
			if json.Unmarshal(reply, &evaluated) == nil {
				if marker, ok := evaluated.Result.Value.(string); ok && marker != "" {
					return []byte(marker), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("%w (last page error: %v)", ctx.Err(), lastErr)
			}
			return nil, ctx.Err()
		case <-session.closed:
			return nil, session.failure()
		case <-time.After(devtoolsPollInterval):
		}
	}
}

// pageTarget waits for Chrome's first page target.
func (session *devtools) pageTarget(ctx context.Context) (string, error) {
	for {
		reply, err := session.call(ctx, "", "Target.getTargets", nil)
		if err != nil {
			return "", err
		}
		var targets struct {
			TargetInfos []struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
			} `json:"targetInfos"`
		}
		if err := json.Unmarshal(reply, &targets); err != nil {
			return "", fmt.Errorf("list targets: %w", err)
		}
		for _, target := range targets.TargetInfos {
			if target.Type == "page" {
				return target.TargetID, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(devtoolsPollInterval):
		}
	}
}

// errDevtoolsCommand marks an error reply to one protocol command.
var errDevtoolsCommand = errors.New("devtools command failed")

// devtools is a minimal client for Chrome's pipe transport: JSON messages
// separated by NUL bytes.
type devtools struct {
	write   io.Writer
	writeMu sync.Mutex

	mu      sync.Mutex
	next    int
	pending map[int]chan devtoolsReply
	err     error
	closed  chan struct{}
}

type devtoolsReply struct {
	result json.RawMessage
	err    error
}

func newDevtools(write io.Writer, read io.Reader) *devtools {
	session := &devtools{write: write, pending: map[int]chan devtoolsReply{}, closed: make(chan struct{})}
	go session.readLoop(read)
	return session
}

func (session *devtools) readLoop(read io.Reader) {
	scanner := bufio.NewScanner(read)
	scanner.Buffer(make([]byte, 64*1024), 256*1024*1024)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if index := bytes.IndexByte(data, 0); index >= 0 {
			return index + 1, data[:index], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for scanner.Scan() {
		var message struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &message) != nil || message.ID == 0 {
			continue // events and unparsable messages
		}
		reply := devtoolsReply{result: message.Result}
		if message.Error != nil {
			reply.err = fmt.Errorf("%w: %s", errDevtoolsCommand, message.Error.Message)
		}
		session.mu.Lock()
		waiter := session.pending[message.ID]
		delete(session.pending, message.ID)
		session.mu.Unlock()
		if waiter != nil {
			waiter <- reply
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	session.mu.Lock()
	session.err = fmt.Errorf("devtools pipe closed: %w", err)
	session.mu.Unlock()
	close(session.closed)
}

func (session *devtools) failure() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.err
}

// call sends one command and waits for its reply.
func (session *devtools) call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	session.mu.Lock()
	if session.err != nil {
		session.mu.Unlock()
		return nil, session.err
	}
	session.next++
	id := session.next
	waiter := make(chan devtoolsReply, 1)
	session.pending[id] = waiter
	session.mu.Unlock()
	defer func() {
		session.mu.Lock()
		delete(session.pending, id)
		session.mu.Unlock()
	}()

	message := map[string]any{"id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	if sessionID != "" {
		message["sessionId"] = sessionID
	}
	data, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	session.writeMu.Lock()
	_, err = session.write.Write(append(data, 0))
	session.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("devtools write %s: %w", method, err)
	}
	select {
	case reply := <-waiter:
		return reply.result, reply.err
	case <-session.closed:
		return nil, session.failure()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// lockedTail keeps the last few kilobytes written by a child process.
type lockedTail struct {
	mu   sync.Mutex
	data []byte
}

func (buffer *lockedTail) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	buffer.data = append(buffer.data, data...)
	if over := len(buffer.data) - 8000; over > 0 {
		buffer.data = append([]byte(nil), buffer.data[over:]...)
	}
	return len(data), nil
}

func (buffer *lockedTail) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return tail(buffer.data)
}
