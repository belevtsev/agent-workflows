package installers

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.host.program, p.hostArgs("app-server")...)
	cmd.Dir, cmd.Env = p.repo, p.env
	cmd.WaitDelay = 3 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Codex app-server: %v", err)
	}
	defer func() {
		// EOF lets the stdio server shut down its helpers and release the
		// disposable profile. The context still bounds an unresponsive server.
		_ = input.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("shut down Codex inventory server: %v", err)
		}
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("Codex app-server stderr: %s", stderr.Bytes())
		}
	}()

	writer, reader := json.NewEncoder(input), json.NewDecoder(output)
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
				t.Fatalf("read Codex inventory response %d: %v (context: %v)", id, err, ctx.Err())
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
