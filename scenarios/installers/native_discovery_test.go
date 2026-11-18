package installers

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

// These inventory commands load the installed plugin in the host without
// starting a conversation. The launch preview and hook trust remain separate.
func (p nativeProfile) discoverTaskSkill(want []byte) string {
	t := p.t
	t.Helper()
	const skill = "kitsu:task"
	if p.host.name == "codex" {
		path := p.codexTaskSkill(skill)
		if !filepath.IsAbs(path) || !bytes.Equal(readFile(t, path), want) {
			t.Fatalf("Codex listed %q at %q, which did not match the verified skill", skill, path)
		}
		return skill
	}
	details := p.hostCommand("plugin", "details", "kitsu@kitsu")
	if !strings.Contains(string(details), "Source: kitsu@kitsu") {
		t.Fatalf("Claude plugin inventory did not identify kitsu@kitsu:\n%s", details)
	}
	for line := range strings.Lines(string(details)) {
		inventory, ok := strings.CutPrefix(strings.TrimSpace(line), "Skills (")
		if !ok {
			continue
		}
		_, names, _ := strings.Cut(inventory, ")")
		for _, name := range strings.Split(names, ",") {
			if strings.TrimSpace(name) == "task" {
				return skill
			}
		}
	}
	t.Fatalf("Claude plugin inventory did not list %s:\n%s", skill, details)
	return ""
}

func (p nativeProfile) codexTaskSkill(name string) string {
	t := p.t
	t.Helper()
	var path string
	runInventoryProcess(t, proc.Spec{
		Path: p.host.program, Args: p.hostArgs("app-server"), Dir: p.repo, Env: p.env, Timeout: 30 * time.Second,
	}, func(stdout io.Reader, stdin io.WriteCloser) {
		path = p.readCodexInventory(stdout, stdin, name)
	})
	return path
}

func runInventoryProcess(t *testing.T, spec proc.Spec, inspect func(io.Reader, io.WriteCloser)) {
	t.Helper()
	childIn, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer childIn.Close()
	defer input.Close()
	output, childOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	defer childOut.Close()
	stderr, err := os.CreateTemp(t.TempDir(), "inventory-stderr-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()

	// Direct file handles let proc.Run observe the server's exit before a
	// helper releases inherited streams. It then reaps the group or Windows
	// job object before the caller removes the disposable profile.
	spec.Stdin, spec.Stdout, spec.Stderr = childIn, childOut, stderr
	done := make(chan proc.Result, 1)
	go func() {
		result := proc.Run(t.Context(), spec)
		_ = childIn.Close()
		_ = childOut.Close()
		if result.Cleanup != nil {
			_ = output.Close()
		}
		done <- result
	}()
	defer func() {
		// EOF permits a clean server exit. proc.Run also bounds hangs and
		// waits for the descendants before delivering the result.
		_ = input.Close()
		result := <-done
		if result.Err != nil || result.Cleanup != nil || result.Stopped || result.Code != 0 {
			t.Errorf("run Codex inventory server: exit=%d stopped=%t error=%v cleanup=%v", result.Code, result.Stopped, result.Err, result.Cleanup)
		}
		if t.Failed() {
			logInventoryStderr(t, stderr)
		}
	}()
	inspect(output, input)
}

func logInventoryStderr(t *testing.T, stderr *os.File) {
	t.Helper()
	const maxBytes = 8192
	info, err := stderr.Stat()
	if err == nil && info.Size() > 0 {
		data, err := io.ReadAll(io.NewSectionReader(stderr, max(0, info.Size()-maxBytes), maxBytes))
		if err == nil {
			t.Logf("Codex app-server stderr: %s", data)
		}
	}
}

func (p nativeProfile) readCodexInventory(stdout io.Reader, stdin io.Writer, name string) string {
	t := p.t
	t.Helper()
	writer, reader := json.NewEncoder(stdin), json.NewDecoder(stdout)
	send := func(message map[string]any) {
		t.Helper()
		if err := writer.Encode(message); err != nil {
			t.Fatalf("send Codex inventory request: %v", err)
		}
	}
	read := func(id int) json.RawMessage {
		t.Helper()
		for {
			var reply struct {
				ID            int
				Result, Error json.RawMessage
			}
			if err := reader.Decode(&reply); err != nil {
				t.Fatalf("read Codex inventory response %d: %v", id, err)
			}
			if reply.ID == id {
				if len(reply.Result) == 0 || len(reply.Error) != 0 {
					t.Fatalf("Codex inventory request %d failed: %s", id, reply.Error)
				}
				return reply.Result
			}
		}
	}
	send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{
		"clientInfo": map[string]string{"name": "kitsu_install_scenario", "version": "1"},
	}})
	read(1)
	send(map[string]any{"method": "initialized"})
	send(map[string]any{"id": 2, "method": "skills/list", "params": map[string]any{"cwds": []string{p.repo}}})
	var inventory struct {
		Data []struct {
			Skills []struct {
				Name, Path string
				Enabled    bool
			}
		}
	}
	result := read(2)
	decodeReceipt(t, result, &inventory)
	for _, row := range inventory.Data {
		for _, skill := range row.Skills {
			if skill.Enabled && skill.Name == name {
				return skill.Path
			}
		}
	}
	t.Fatalf("Codex skills/list did not report enabled %s:\n%s", name, result)
	return ""
}
