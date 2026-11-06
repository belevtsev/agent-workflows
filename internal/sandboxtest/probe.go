package sandboxtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

const artifactProbeSecret = "artifact-probe-synthetic-not-a-real-key"

type artifactProbeHome struct {
	root, home, codex, claude, state, config, work, tools, ca string
	codexOriginal, claudeOriginal                             []byte
}
type artifactProbeCapture struct {
	mutex          sync.Mutex
	stdout, stderr []byte
	exceeded       bool
}
type artifactProbeStream struct {
	capture *artifactProbeCapture
	stderr  bool
}

func (stream artifactProbeStream) Write(data []byte) (int, error) {
	stream.capture.mutex.Lock()
	defer stream.capture.mutex.Unlock()
	if len(stream.capture.stdout)+len(stream.capture.stderr)+len(data) > 1<<20 {
		stream.capture.exceeded = true
		return 0, errors.New("output-limit")
	}
	if stream.stderr {
		stream.capture.stderr = append(stream.capture.stderr, data...)
	} else {
		stream.capture.stdout = append(stream.capture.stdout, data...)
	}
	return len(data), nil
}
func artifactProbeWrite(path string, data []byte, mode os.FileMode) error {
	if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, data, mode) != nil {
		return errors.New("fixture-write")
	}
	return nil
}
func artifactProbeHash(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func artifactProbeMap(value any) map[string]any { result, _ := value.(map[string]any); return result }
func artifactProbeString(value any) string      { result, _ := value.(string); return result }
func artifactProbeDigest(value any) bool {
	text := artifactProbeString(value)
	decoded, err := hex.DecodeString(text)
	return err == nil && len(decoded) == 32 && text == strings.ToLower(text)
}
func artifactProbeEffectiveRelease(value any) bool {
	digest, prefixed := strings.CutPrefix(artifactProbeString(value), "effective-")
	return prefixed && artifactProbeDigest(digest)
}
func artifactProbeJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	var result map[string]any
	if err != nil || json.Unmarshal(data, &result) != nil || result == nil {
		return nil, errors.New("json-file")
	}
	return result, nil
}
func artifactProbeNewHome(root, ca string) (*artifactProbeHome, error) {
	home := &artifactProbeHome{root: root, home: filepath.Join(root, "home with spaces"), codex: filepath.Join(root, "codex with spaces"), claude: filepath.Join(root, "claude with spaces"), state: filepath.Join(root, "state with spaces"), config: filepath.Join(root, "config with spaces"), work: filepath.Join(root, "unrelated cwd"), tools: filepath.Join(root, "tools"), ca: ca}
	home.codexOriginal = []byte("unrelated = \"keep  spacing\"\nmodel = \"original-codex\"\nmodel_reasoning_effort = \"low\"\n[artifact_probe]\nnote = \"untouched\"\n")
	home.claudeOriginal = []byte("{\n  \"model\": \"original-claude\",\n  \"effortLevel\": \"low\",\n  \"artifact_probe\": { \"spacing\" : \"keep\" }\n}\n")
	for _, directory := range []string{home.home, home.codex, home.claude, home.state, home.config, home.work, home.tools, filepath.Join(root, "tmp")} {
		if os.MkdirAll(directory, 0700) != nil {
			return nil, errors.New("fixture-directory")
		}
	}
	for _, fixture := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{{filepath.Join(home.codex, "config.toml"), home.codexOriginal, 0640}, {filepath.Join(home.claude, "settings.json"), home.claudeOriginal, 0640}, {filepath.Join(home.home, ".agents", "skills", "unmanaged-probe", "SKILL.md"), []byte("unmanaged sentinel\n"), 0600}, {filepath.Join(home.claude, "skills", "unmanaged-probe", "SKILL.md"), []byte("unmanaged sentinel\n"), 0600}} {
		if err := artifactProbeWrite(fixture.path, fixture.data, fixture.mode); err != nil {
			return nil, err
		}
	}
	marker := "'" + strings.ReplaceAll(filepath.Join(root, "forbidden-tools"), "'", "'\\''") + "'"
	for _, tool := range []string{"git", "go"} {
		if err := artifactProbeWrite(filepath.Join(home.tools, tool), []byte("#!/bin/sh\nprintf 'called\\n' >> "+marker+"\nexit 93\n"), 0700); err != nil {
			return nil, err
		}
	}
	return home, nil
}
func (home *artifactProbeHome) artifactProbeEnv() []string {
	return []string{"HOME=" + home.home, "CODEX_HOME=" + home.codex, "CLAUDE_CONFIG_DIR=" + home.claude, "XDG_CONFIG_HOME=" + filepath.Join(home.root, "xdg-config"), "XDG_DATA_HOME=" + filepath.Join(home.root, "xdg-data"), "XDG_STATE_HOME=" + filepath.Join(home.root, "xdg-state"), "XDG_CACHE_HOME=" + filepath.Join(home.root, "xdg-cache"), "PATH=" + home.tools + ":/usr/bin:/bin", "TERM=dumb", "LC_ALL=C", "SSL_CERT_FILE=" + home.ca, "TMPDIR=" + filepath.Join(home.root, "tmp"), "TMP=" + filepath.Join(home.root, "tmp"), "TEMP=" + filepath.Join(home.root, "tmp")}
}
func (home *artifactProbeHome) artifactProbeRun(ctx context.Context, binary string, arguments, overrides []string) ([]byte, bool, error) {
	child, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	capture := &artifactProbeCapture{}
	command := exec.CommandContext(child, binary, arguments...)
	command.Dir = home.work
	command.Env = home.artifactProbeEnv()
	for _, override := range overrides {
		name, _, _ := strings.Cut(override, "=")
		filtered := command.Env[:0]
		for _, entry := range command.Env {
			if !strings.HasPrefix(entry, name+"=") {
				filtered = append(filtered, entry)
			}
		}
		command.Env = append(filtered, override)
	}
	command.Stdin = strings.NewReader("")
	command.Stdout = artifactProbeStream{capture: capture}
	command.Stderr = artifactProbeStream{capture: capture, stderr: true}
	command.WaitDelay = time.Second
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	runErr := command.Run()
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if child.Err() != nil {
		return nil, false, errors.New("child-deadline")
	}
	if capture.exceeded {
		return nil, false, errors.New("output-limit")
	}
	if bytes.Contains(capture.stdout, []byte(artifactProbeSecret)) || bytes.Contains(capture.stderr, []byte(artifactProbeSecret)) {
		return nil, false, errors.New("credential-disclosure")
	}
	if bytes.Contains(capture.stdout, []byte{27}) || bytes.Contains(capture.stderr, []byte{27}) {
		return nil, false, errors.New("unexpected-ui")
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return nil, false, errors.New("child-start")
		}
		return capture.stdout, false, nil
	}
	return capture.stdout, true, nil
}
func (home *artifactProbeHome) artifactProbeArgs(action string, rooted bool, extra ...string) []string {
	arguments := []string{action, "--json", "--non-interactive"}
	if rooted {
		arguments = append(arguments, "--home", home.home, "--codex-home", home.codex, "--claude-home", home.claude, "--state-dir", home.state, "--config-dir", home.config)
	}
	return append(arguments, extra...)
}
func (home *artifactProbeHome) artifactProbeAction(ctx context.Context, binary, action string, rooted bool, extra ...string) (map[string]any, error) {
	data, success, err := home.artifactProbeRun(ctx, binary, home.artifactProbeArgs(action, rooted, extra...), nil)
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, errors.New("action-failed")
	}
	var result map[string]any
	if json.Unmarshal(data, &result) != nil || result["action"] != action {
		return nil, errors.New("action-json")
	}
	actionResult, resultObject := result["result"].(map[string]any)
	if !resultObject || actionResult == nil {
		return nil, errors.New("result-object")
	}
	status, statusObject := result["status"].(map[string]any)
	if !statusObject || status == nil {
		return nil, errors.New("status-object")
	}
	committed, present := actionResult["configuration_committed"]
	if _, valid := committed.(bool); !valid && (present || action == "install" || action == "setup" || action == "update") {
		return nil, errors.New("commit-indicator")
	}
	if _, valid := status["installed"].(bool); !valid {
		return nil, errors.New("installed-indicator")
	}
	return result, nil
}
func (home *artifactProbeHome) artifactProbePreserved(original bool) error {
	for _, fixture := range []struct {
		path               string
		original, sentinel []byte
	}{{filepath.Join(home.codex, "config.toml"), home.codexOriginal, []byte("unrelated = \"keep  spacing\"\n")}, {filepath.Join(home.claude, "settings.json"), home.claudeOriginal, []byte("  \"artifact_probe\": { \"spacing\" : \"keep\" }")}} {
		data, err := os.ReadFile(fixture.path)
		info, statErr := os.Stat(fixture.path)
		if err != nil || statErr != nil || info.Mode().Perm() != 0640 || !bytes.Contains(data, fixture.sentinel) || (original && !bytes.Equal(data, fixture.original)) {
			return errors.New("settings-preservation")
		}
	}
	for _, directory := range []string{filepath.Join(home.home, ".agents", "skills"), filepath.Join(home.claude, "skills")} {
		path := filepath.Join(directory, "unmanaged-probe", "SKILL.md")
		data, err := os.ReadFile(path)
		info, statErr := os.Stat(path)
		directoryInfo, directoryErr := os.Stat(filepath.Dir(path))
		if err != nil || statErr != nil || directoryErr != nil || info.Mode().Perm() != 0600 || directoryInfo.Mode().Perm() != 0700 || string(data) != "unmanaged sentinel\n" {
			return errors.New("unmanaged-preservation")
		}
	}
	if _, err := os.Stat(filepath.Join(home.root, "forbidden-tools")); !os.IsNotExist(err) {
		return errors.New("git-go-invocation")
	}
	return nil
}
func (home *artifactProbeHome) artifactProbeVerify(manager, base map[string]any, pinned bool) error {
	state, err := artifactProbeJSON(filepath.Join(home.state, "state.json"))
	if err != nil {
		return err
	}
	runtimeState := artifactProbeMap(artifactProbeMap(state["manager"])["runtime"])
	if state["version"] != float64(4) || state["config_dir"] != home.config || !artifactProbeEffectiveRelease(state["release"]) || artifactProbeMap(state["manager"])["version"] != float64(2) {
		return errors.New("state-contract")
	}
	if _, valid := state["history"].([]any); !valid {
		return errors.New("state-history")
	}
	for _, key := range []string{"version", "revision", "os", "arch", "base_digest"} {
		if artifactProbeMap(state["base"])[key] != base[key] {
			return errors.New("base-identity")
		}
	}
	for _, key := range []string{"version", "revision", "os", "arch"} {
		if runtimeState[key] != manager[key] {
			return errors.New("manager-identity")
		}
	}
	current := filepath.Join(home.state, "current")
	info, err := os.Lstat(current)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return errors.New("active-symlink")
	}
	if _, err := filepath.EvalSymlinks(current); err != nil {
		return errors.New("active-target")
	}
	codexData, err := os.ReadFile(filepath.Join(home.codex, "config.toml"))
	var codex map[string]any
	if err != nil || toml.Unmarshal(codexData, &codex) != nil {
		return errors.New("codex-toml")
	}
	claude, err := artifactProbeJSON(filepath.Join(home.claude, "settings.json"))
	if err != nil {
		return err
	}
	codexEffort, claudeEffort, window, enabled := "max", "high", int64(512000), true
	if pinned {
		codexEffort, claudeEffort, window, enabled = "xhigh", "high", 384000, false
	}
	if codex["model"] != "gpt-6.1-sol" || codex["model_reasoning_effort"] != codexEffort || codex["model_context_window"] != window || claude["model"] != "claude-opus-5-5" || claude["effortLevel"] != claudeEffort {
		return errors.New("native-models")
	}
	memories := artifactProbeMap(codex["memories"])
	if artifactProbeMap(codex["features"])["memories"] != enabled || memories["generate_memories"] != enabled || memories["use_memories"] != enabled || memories["extract_model"] != "gpt-6.1-sol" || memories["consolidation_model"] != "gpt-6.1-sol" {
		return errors.New("native-memory")
	}
	for _, host := range []struct{ file, model, effort, boundedModel, boundedEffort, effortKey string }{{"model-policy.yaml", "gpt-6.1-sol", codexEffort, "gpt-6-luna", "high", "reasoning_effort"}, {"model-policy.claude.yaml", "claude-opus-5-5", claudeEffort, "claude-sonnet-5-5", "medium", "effort"}} {
		data, readErr := os.ReadFile(filepath.Join(current, "skills", "task-orchestration", host.file))
		var policy map[string]any
		if readErr != nil || yaml.Unmarshal(data, &policy) != nil {
			return errors.New("policy-yaml")
		}
		roles, profiles := artifactProbeMap(policy["roles"]), artifactProbeMap(policy["profiles"])
		for _, role := range []string{"coordinator", "commit_execution", "pr_authoring_routine", "pr_submission"} {
			model, effort := host.model, host.effort
			if role != "coordinator" {
				model, effort = host.boundedModel, host.boundedEffort
			}
			profile := artifactProbeMap(profiles[artifactProbeString(roles[role])])
			if profile["model"] != model || profile[host.effortKey] != effort || artifactProbeString(profile["scope"]) == "" {
				return errors.New("policy-role")
			}
		}
		if host.effortKey == "effort" && !pinned {
			// The shipped (unpinned) Claude policy keeps xhigh only for the
			// decision roles; this distinguishes it from the pinned preset now
			// that both use high for the coordinator.
			for _, role := range []string{"planning", "architecture", "diagnosis"} {
				name := artifactProbeString(roles[role])
				profile := artifactProbeMap(profiles[name])
				if name != "decision" || profile["model"] != "claude-opus-5-5" || profile["effort"] != "xhigh" || artifactProbeString(profile["scope"]) != "substantive" {
					return errors.New("policy-role")
				}
			}
		}
	}
	for _, directory := range []string{filepath.Join(home.home, ".agents", "skills"), filepath.Join(home.claude, "skills")} {
		for _, skill := range []string{"task-orchestration", "test-strategy"} {
			path := filepath.Join(directory, skill)
			link, linkErr := os.Lstat(path)
			target, resolveErr := filepath.EvalSymlinks(path)
			targetInfo, statErr := os.Stat(target)
			if linkErr != nil || resolveErr != nil || statErr != nil || link.Mode()&os.ModeSymlink == 0 || !targetInfo.IsDir() {
				return errors.New("critical-skill-link")
			}
		}
	}
	return home.artifactProbePreserved(false)
}
func (home *artifactProbeHome) artifactProbeSnapshot() (map[string]string, error) {
	result := map[string]string{}
	for _, path := range []string{filepath.Join(home.state, "state.json"), filepath.Join(home.config, "preferences.json"), filepath.Join(home.config, "credentials.json"), filepath.Join(home.codex, "config.toml"), filepath.Join(home.claude, "settings.json"), filepath.Join(home.state, "current", "skills", "task-orchestration", "model-policy.yaml"), filepath.Join(home.state, "current", "skills", "task-orchestration", "model-policy.claude.yaml")} {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			result[path] = "absent"
			continue
		}
		info, statErr := os.Stat(path)
		if err != nil || statErr != nil {
			return nil, errors.New("snapshot-read")
		}
		result[path] = artifactProbeHash(data) + fmt.Sprint(info.Mode().Perm())
	}
	target, err := os.Readlink(filepath.Join(home.state, "current"))
	if os.IsNotExist(err) {
		target = "absent"
	} else if err != nil {
		return nil, errors.New("snapshot-current")
	}
	result["current"] = artifactProbeHash([]byte(target))
	return result, nil
}
func (home *artifactProbeHome) artifactProbeRefusal(ctx context.Context, binary string, arguments, overrides []string) error {
	before, err := home.artifactProbeSnapshot()
	if err != nil {
		return err
	}
	_, success, err := home.artifactProbeRun(ctx, binary, arguments, overrides)
	if err != nil {
		return err
	}
	if success {
		return errors.New("expected-refusal")
	}
	after, err := home.artifactProbeSnapshot()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("refusal-mutated-active")
	}
	return nil
}

type artifactProbeLab struct {
	mutex                       sync.Mutex
	archives                    [2][]byte
	hashes                      [2]string
	ca, revision, version, mode string
	index                       int
	requests                    map[string]int
	violation, verified         bool
}

func artifactProbeArchive(binary []byte) ([]byte, error) {
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	if archive.WriteHeader(&tar.Header{Name: "cw", Mode: 0755, Size: int64(len(binary)), Typeflag: tar.TypeReg}) != nil {
		return nil, errors.New("archive-header")
	}
	if _, err := archive.Write(binary); err != nil {
		return nil, errors.New("archive-body")
	}
	if archive.Close() != nil || compressed.Close() != nil {
		return nil, errors.New("archive-close")
	}
	return buffer.Bytes(), nil
}
func artifactProbeNewLab(ctx context.Context, root, first, second string) (*artifactProbeLab, func(), error) {
	lab := &artifactProbeLab{ca: filepath.Join(root, "synthetic-ca.pem"), requests: map[string]int{}, mode: "offline"}
	for index, path := range []string{first, second} {
		binary, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, errors.New("input-read")
		}
		lab.hashes[index] = artifactProbeHash(binary)
		lab.archives[index], err = artifactProbeArchive(binary)
		if err != nil {
			return nil, nil, err
		}
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, errors.New("ca-key")
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, errors.New("leaf-key")
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "artifact probe CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, errors.New("ca-certificate")
	}
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"github.com", "api.github.com"}, NotBefore: caTemplate.NotBefore, NotAfter: caTemplate.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, errors.New("leaf-certificate")
	}
	if err := artifactProbeWrite(lab.ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0644); err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:443")
	if err != nil {
		return nil, nil, errors.New("https-listen")
	}
	server := &http.Server{Handler: lab, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 90 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	secure := tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}}})
	done, stop, watcher := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { defer close(done); _ = server.Serve(secure) }()
	go func() {
		defer close(watcher)
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-stop:
		}
	}()
	cleanup := func() {
		close(stop)
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		_ = server.Close()
		<-done
		<-watcher
	}
	return lab, cleanup, nil
}
func (lab *artifactProbeLab) artifactProbePlan(version string, index int, mode string) {
	lab.mutex.Lock()
	defer lab.mutex.Unlock()
	lab.version, lab.index, lab.mode, lab.requests, lab.violation = version, index, mode, map[string]int{}, false
}
func (lab *artifactProbeLab) artifactProbeAudit(download bool) error {
	lab.mutex.Lock()
	defer lab.mutex.Unlock()
	if lab.violation {
		return errors.New("https-unexpected-or-retried-request")
	}
	if lab.mode == "offline" {
		if len(lab.requests) != 0 {
			return errors.New("offline-http")
		}
		return nil
	}
	if lab.requests["/repos/belevtsev/agent-workflows/releases/latest"] != 1 {
		return errors.New("latest-pin-count")
	}
	if download {
		for _, path := range []string{"/repos/belevtsev/agent-workflows/git/ref/tags/" + lab.version, "/belevtsev/agent-workflows/releases/download/" + lab.version + "/SHA256SUMS", "/belevtsev/agent-workflows/releases/download/" + lab.version + "/cw_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"} {
			if lab.requests[path] != 1 {
				return errors.New("pinned-download-count")
			}
		}
		if lab.mode == "good" {
			lab.verified = true
		}
	}
	return nil
}
func (lab *artifactProbeLab) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	lab.mutex.Lock()
	defer lab.mutex.Unlock()
	path := request.URL.Path
	lab.requests[path]++
	if request.Method != http.MethodGet || request.URL.RawQuery != "" || lab.requests[path] != 1 || lab.mode == "offline" {
		lab.violation = true
		response.WriteHeader(http.StatusConflict)
		return
	}
	prefix := "/belevtsev/agent-workflows/releases/download/" + lab.version + "/"
	native := "cw_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	sums := ""
	assets := []map[string]any{}
	for _, platform := range []string{"darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64"} {
		name := "cw_" + platform + ".tar.gz"
		payload := []byte("opaque-unserved-platform")
		if name == native {
			payload = lab.archives[lab.index]
		}
		sums += artifactProbeHash(payload) + "  " + name + "\n"
		assets = append(assets, map[string]any{"id": len(assets) + 1, "name": name, "size": len(payload), "state": "uploaded", "browser_download_url": "https://github.com" + prefix + name})
	}
	assets = append(assets, map[string]any{"id": 5, "name": "SHA256SUMS", "size": len(sums), "state": "uploaded", "browser_download_url": "https://github.com" + prefix + "SHA256SUMS"})
	response.Header().Set("Content-Type", "application/json")
	switch path {
	case "/repos/belevtsev/agent-workflows/releases/latest":
		if request.Host != "api.github.com" {
			lab.violation = true
			response.WriteHeader(400)
			return
		}
		revision := lab.revision
		if lab.mode == "bad-source" {
			revision = "0" + lab.revision[1:]
			if revision == lab.revision {
				revision = "1" + lab.revision[1:]
			}
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"id": 1002, "tag_name": lab.version, "target_commitish": revision, "draft": false, "prerelease": false, "assets": assets})
	case "/repos/belevtsev/agent-workflows/git/ref/tags/" + lab.version:
		if request.Host != "api.github.com" {
			lab.violation = true
			response.WriteHeader(400)
			return
		}
		ref := "refs/tags/" + lab.version
		if lab.mode == "bad-tag" {
			ref = "refs/tags/v0.0.999"
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"ref": ref, "object": map[string]any{"type": "commit", "sha": lab.revision}})
	case prefix + "SHA256SUMS":
		if request.Host != "github.com" {
			lab.violation = true
			response.WriteHeader(400)
			return
		}
		response.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(response, sums)
	case prefix + native:
		if request.Host != "github.com" {
			lab.violation = true
			response.WriteHeader(400)
			return
		}
		response.Header().Set("Content-Type", "application/gzip")
		_, _ = response.Write(lab.archives[lab.index])
	default:
		lab.violation = true
		response.WriteHeader(http.StatusNotFound)
	}
}

// artifactProbeWorkspace owns only its private child and, when created here, an empty root.
func artifactProbeWorkspace(root string) (string, func(), error) {
	root = filepath.Clean(root)
	createdRoot := false
	rootInfo, err := os.Lstat(root)
	if os.IsNotExist(err) {
		if err = os.Mkdir(root, 0700); err != nil {
			return "", nil, errors.New("workspace-root-create")
		}
		createdRoot = true
		rootInfo, err = os.Lstat(root)
	}
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || rootInfo.Mode().Perm()&0022 != 0 || (createdRoot && rootInfo.Mode().Perm() != 0700) {
		if createdRoot {
			_ = os.Remove(root)
		}
		return "", nil, errors.New("workspace-root-unsafe")
	}
	cleanupRoot := func() {
		if createdRoot {
			current, statErr := os.Lstat(root)
			if statErr == nil && os.SameFile(rootInfo, current) {
				_ = os.Remove(root)
			}
		}
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		cleanupRoot()
		return "", nil, errors.New("workspace-root-physical")
	}
	workspace, err := os.MkdirTemp(physicalRoot, "artifact-probe-")
	if err != nil {
		cleanupRoot()
		return "", nil, errors.New("workspace")
	}
	workspaceInfo, statErr := os.Lstat(workspace)
	physicalWorkspace, resolveErr := filepath.EvalSymlinks(workspace)
	cleanup := func() {
		current, currentErr := os.Lstat(workspace)
		if statErr == nil && currentErr == nil && os.SameFile(workspaceInfo, current) {
			_ = os.RemoveAll(workspace)
		}
		cleanupRoot()
	}
	if statErr != nil || resolveErr != nil || !workspaceInfo.IsDir() || workspaceInfo.Mode().Perm() != 0700 {
		cleanup()
		return "", nil, errors.New("workspace-private-physical")
	}
	return physicalWorkspace, cleanup, nil
}

// artifactProbeFailureCodes exposes only fixed case IDs and explicitly allowed category codes.
func artifactProbeFailureCodes(err error) (string, string) {
	const fallbackCase, fallbackCategory = "probe", "unclassified"
	if err == nil {
		return fallbackCase, fallbackCategory
	}
	message := err.Error()
	if len(message) > 256 {
		return fallbackCase, fallbackCategory
	}
	for _, caseID := range []string{"R-A1", "R-A2/preferences", "R-A2/update", "R-A3/provenance-refusals", "R-A3/roots-drift", "R-A2/offline-rollback", "R-A3/recovery-uninstall", "R-A4"} {
		category, matched := strings.CutPrefix(message, caseID+"/")
		if !matched {
			continue
		}
		stage := ""
		if prefix, code, scoped := strings.Cut(category, "/"); scoped {
			if caseID != "R-A3/provenance-refusals" {
				return caseID, fallbackCategory
			}
			switch prefix {
			case "older", "bad-tag", "bad-source", "bad-payload-metadata":
				stage, category = prefix+"/", code
			default:
				return caseID, fallbackCategory
			}
		}
		switch category {
		case "action-failed", "action-json", "active-symlink", "active-target", "archive-body",
			"archive-close", "archive-header", "base-digest", "base-identity", "bundle-json",
			"ca-certificate", "ca-key", "child-deadline", "child-start", "codex-toml",
			"commit-indicator", "credential-disclosure", "credential-presence-source", "critical-skill-link", "drift-fixture-shape",
			"drift-read", "embedded-identity", "environment", "expected-refusal", "expected-revision",
			"fault-not-triggered", "fault-uninstall-outcome", "fixture-directory", "fixture-write", "git-go-invocation",
			"https-listen", "https-not-exercised", "https-unexpected-or-retried-request", "input-path", "input-provenance",
			"input-read", "install-outcome", "installed-indicator", "json-file", "keys-read",
			"latest-pin-count", "leaf-certificate", "leaf-key", "manager-identity", "manager-protocol",
			"native-memory", "native-models", "offline-http", "output-limit", "pending-setup-not-refused",
			"pinned-download-count", "policy-role", "policy-yaml", "preferences-read", "preferences-replacement",
			"preferences-version", "probe-deadline", "recovery-active-outcome", "recovery-preferences", "refusal-mutated-active",
			"result-object", "rollback-preferences", "sealed-config-discovery", "settings-preservation", "snapshot-current",
			"snapshot-read", "state-contract", "state-history", "status-object", "unexpected-ui",
			"uninstall-outcome", "uninstall-preferences-keys", "unmanaged-preservation", "update-outcome", "update-preferences-effective",
			"version-contract", "version-json", "workspace", "workspace-private-physical", "workspace-root-create",
			"workspace-root-physical", "workspace-root-unsafe", "xdg-relocation":
			return caseID, stage + category
		default:
			return caseID, fallbackCategory
		}
	}
	return fallbackCase, fallbackCategory
}

func Probe(ctx context.Context, first, second, root, expectedRevision string) (report map[string]any, probeErr error) {
	report = map[string]any{"status": "failure", "cases": []string{}, "coverage": map[string]any{"native_https_asset": false, "four_platform_verification": false, "ui_credential_writing": false, "equal_version_order_unit": false, "historical_compatibility": false, "full_skill_inventory": false, "balanced_all_roles": false}}
	defer func() {
		if probeErr != nil {
			failedCase, category := artifactProbeFailureCodes(probeErr)
			report["failed_case"] = failedCase
			report["failure_category"] = category
		}
	}()
	if !filepath.IsAbs(root) || runtime.GOOS != "linux" {
		return report, errors.New("R-A1/environment")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(expectedRevision) {
		return report, errors.New("R-A1/expected-revision")
	}
	workspace, cleanupWorkspace, err := artifactProbeWorkspace(root)
	if err != nil {
		return report, fmt.Errorf("R-A1/%s", err.Error())
	}
	defer cleanupWorkspace()
	first, err = filepath.Abs(first)
	if err != nil {
		return report, errors.New("R-A1/input-path")
	}
	second, err = filepath.Abs(second)
	if err != nil {
		return report, errors.New("R-A1/input-path")
	}
	lab, cleanup, err := artifactProbeNewLab(ctx, workspace, first, second)
	if err != nil {
		return report, fmt.Errorf("R-A4/%s", err.Error())
	}
	defer cleanup()
	home, err := artifactProbeNewHome(filepath.Join(workspace, "main"), lab.ca)
	if err != nil {
		return report, fmt.Errorf("R-A1/%s", err.Error())
	}
	identities := [2]map[string]any{}
	for index, binary := range []string{first, second} {
		data, success, runErr := home.artifactProbeRun(ctx, binary, []string{"version"}, nil)
		var version map[string]any
		if runErr != nil || !success || json.Unmarshal(data, &version) != nil {
			return report, errors.New("R-A1/version-json")
		}
		expected := []string{"v0.0.101", "v0.0.102"}[index]
		if version["version"] != expected || version["os"] != runtime.GOOS || version["arch"] != runtime.GOARCH || version["revision"] != expectedRevision || artifactProbeString(version["built_at"]) == "" {
			return report, errors.New("R-A1/version-contract")
		}
		data, success, runErr = home.artifactProbeRun(ctx, binary, []string{"--manager-protocol"}, nil)
		if runErr != nil || !success || strings.TrimSpace(string(data)) != "cw-manager-v10" {
			return report, errors.New("R-A1/manager-protocol")
		}
		data, success, runErr = home.artifactProbeRun(ctx, binary, []string{"--bundle-identity"}, nil)
		if runErr != nil || !success || json.Unmarshal(data, &identities[index]) != nil {
			return report, errors.New("R-A1/bundle-json")
		}
		identity := identities[index]
		for _, key := range []string{"version", "revision", "os", "arch"} {
			if identity[key] != version[key] {
				return report, errors.New("R-A1/embedded-identity")
			}
		}
		if !artifactProbeDigest(identity["base_digest"]) {
			return report, errors.New("R-A1/base-digest")
		}
	}
	if identities[0]["revision"] != expectedRevision || identities[1]["revision"] != expectedRevision || identities[0]["base_digest"] == identities[1]["base_digest"] {
		return report, errors.New("R-A1/input-provenance")
	}
	lab.revision = expectedRevision
	report["input_sha256"] = map[string]string{"first": lab.hashes[0], "second": lab.hashes[1]}
	installed := filepath.Join(home.home, ".local", "bin", "cw")
	settings := filepath.Join(workspace, "settings.json")
	full := []byte(`{"version":1,"hosts":{"codex":{"roles":{"coordinator":{"model":"gpt-6.1-sol","effort":"xhigh"}}},"claude":{"roles":{"coordinator":{"model":"claude-opus-5-5","effort":"high"}}}},"shared":{"model_context_window":384000,"features.memories":false,"memories.generate_memories":false,"memories.use_memories":false,"memories.extract_model":"gpt-6.1-sol","memories.consolidation_model":"gpt-6.1-sol"}}`)
	var preferences []byte
	var initialRelease any
	cases := []struct {
		id  string
		run func() error
	}{
		{"R-A1", func() error {
			result, err := home.artifactProbeAction(ctx, first, "install", true, "--host", "all", "--preset", "recommended", "--shell", "none")
			if err != nil {
				return err
			}
			if artifactProbeMap(result["status"])["installed"] != true || artifactProbeMap(result["result"])["configuration_committed"] != true {
				return errors.New("install-outcome")
			}
			if err := home.artifactProbeVerify(identities[0], identities[0], false); err != nil {
				return err
			}
			state, err := artifactProbeJSON(filepath.Join(home.state, "state.json"))
			if err != nil {
				return err
			}
			initialRelease = state["release"]
			if err := artifactProbeWrite(filepath.Join(home.config, "credentials.json"), []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":"`+artifactProbeSecret+`"}}`), 0600); err != nil {
				return err
			}
			result, err = home.artifactProbeAction(ctx, installed, "status", false)
			if err != nil {
				return err
			}
			source := artifactProbeMap(artifactProbeMap(result["credential_sources"])["TYPESAFE_API_KEY"])
			if artifactProbeMap(result["status"])["installed"] != true || source["environment"] != false || source["stored"] != true || source["effective"] != true || artifactProbeString(source["source"]) == "" {
				return errors.New("credential-presence-source")
			}
			return home.artifactProbePreserved(false)
		}},
		{"R-A2/preferences", func() error {
			for _, step := range []struct {
				data   []byte
				preset string
				pinned bool
			}{{full, "", true}, {[]byte(`{"version":1,"hosts":{},"shared":{}}`), "", false}, {full, "", true}, {nil, "recommended", false}, {full, "", true}} {
				extra := []string{"--host", "all", "--shell", "none"}
				if step.preset != "" {
					extra = append(extra, "--preset", step.preset)
				} else {
					if err := artifactProbeWrite(settings, step.data, 0600); err != nil {
						return err
					}
					extra = append(extra, "--settings", settings)
				}
				if _, err := home.artifactProbeAction(ctx, first, "install", true, extra...); err != nil {
					return err
				}
				if err := home.artifactProbeVerify(identities[0], identities[0], step.pinned); err != nil {
					return err
				}
				prefs, err := artifactProbeJSON(filepath.Join(home.config, "preferences.json"))
				if err != nil {
					return err
				}
				expected := map[string]any{}
				data := step.data
				if step.preset != "" {
					data = []byte(`{"version":1,"hosts":{},"shared":{}}`)
				}
				_ = json.Unmarshal(data, &expected)
				if prefs["version"] != float64(1) {
					return errors.New("preferences-version")
				}
				for _, key := range []string{"hosts", "shared"} {
					if !reflect.DeepEqual(prefs[key], expected[key]) && !(len(artifactProbeMap(prefs[key])) == 0 && len(artifactProbeMap(expected[key])) == 0) {
						return errors.New("preferences-replacement")
					}
				}
			}
			var err error
			preferences, err = os.ReadFile(filepath.Join(home.config, "preferences.json"))
			if err != nil {
				return errors.New("preferences-read")
			}
			return nil
		}},
		{"R-A2/update", func() error {
			lab.artifactProbePlan("v0.0.102", 1, "good")
			result, err := home.artifactProbeAction(ctx, installed, "update", false)
			if err != nil {
				return err
			}
			if artifactProbeMap(result["status"])["installed"] != true {
				return errors.New("update-outcome")
			}
			if err := lab.artifactProbeAudit(true); err != nil {
				return err
			}
			if err := home.artifactProbeVerify(identities[1], identities[1], true); err != nil {
				return err
			}
			state, err := artifactProbeJSON(filepath.Join(home.state, "state.json"))
			if err != nil {
				return err
			}
			saved, err := os.ReadFile(filepath.Join(home.config, "preferences.json"))
			if err != nil || !bytes.Equal(saved, preferences) || state["release"] == initialRelease {
				return errors.New("update-preferences-effective")
			}
			return nil
		}},
		{"R-A3/provenance-refusals", func() error {
			for _, fixture := range []struct {
				version, mode string
				index         int
				download      bool
			}{{"v0.0.101", "older", 0, false}, {"v0.0.103", "bad-tag", 1, false}, {"v0.0.103", "bad-source", 1, false}, {"v0.0.103", "bad-payload-metadata", 1, true}} {
				lab.artifactProbePlan(fixture.version, fixture.index, fixture.mode)
				if err := home.artifactProbeRefusal(ctx, installed, home.artifactProbeArgs("update", false), nil); err != nil {
					return fmt.Errorf("%s/%s", fixture.mode, err.Error())
				}
				if err := lab.artifactProbeAudit(fixture.download); err != nil {
					return fmt.Errorf("%s/%s", fixture.mode, err.Error())
				}
			}
			return nil
		}},
		{"R-A3/roots-drift", func() error {
			data, success, err := home.artifactProbeRun(ctx, installed, home.artifactProbeArgs("status", false), []string{"XDG_CONFIG_HOME=" + filepath.Join(workspace, "changed xdg")})
			var status map[string]any
			if err != nil || !success || json.Unmarshal(data, &status) != nil || artifactProbeMap(status["status"])["installed"] != true {
				return errors.New("sealed-config-discovery")
			}
			if _, err := os.Stat(filepath.Join(workspace, "changed xdg")); !os.IsNotExist(err) {
				return errors.New("xdg-relocation")
			}
			conflict := *home
			conflict.config = filepath.Join(workspace, "conflicting config")
			if err := home.artifactProbeRefusal(ctx, installed, conflict.artifactProbeArgs("install", true, "--host", "all", "--shell", "none"), nil); err != nil {
				return err
			}
			path := filepath.Join(home.codex, "config.toml")
			prior, err := os.ReadFile(path)
			if err != nil {
				return errors.New("drift-read")
			}
			expression := regexp.MustCompile(`(?m)^model[ \t]*=[^\r\n]*`)
			if len(expression.FindAll(prior, -1)) != 1 {
				return errors.New("drift-fixture-shape")
			}
			tampered := expression.ReplaceAll(prior, []byte(`model = "artifact-probe-user-model"`))
			if err := artifactProbeWrite(path, tampered, 0640); err != nil {
				return err
			}
			refusal := home.artifactProbeRefusal(ctx, installed, home.artifactProbeArgs("install", true, "--host", "all", "--shell", "none"), nil)
			restore := artifactProbeWrite(path, prior, 0640)
			if refusal != nil {
				return refusal
			}
			return restore
		}},
		{"R-A2/offline-rollback", func() error {
			lab.artifactProbePlan("v0.0.101", 0, "offline")
			if _, err := home.artifactProbeAction(ctx, installed, "rollback", false); err != nil {
				return err
			}
			if err := lab.artifactProbeAudit(false); err != nil {
				return err
			}
			if err := home.artifactProbeVerify(identities[1], identities[0], true); err != nil {
				return err
			}
			saved, err := os.ReadFile(filepath.Join(home.config, "preferences.json"))
			if err != nil || !bytes.Equal(saved, preferences) {
				return errors.New("rollback-preferences")
			}
			return nil
		}},
		{"R-A3/recovery-uninstall", func() error {
			fault, err := artifactProbeNewHome(filepath.Join(workspace, "fault"), lab.ca)
			if err != nil {
				return err
			}
			arguments := fault.artifactProbeArgs("install", true, "--host", "all", "--shell", "none", "--preset", "recommended")
			_, success, err := fault.artifactProbeRun(ctx, first, arguments, []string{"AGENT_WORKFLOWS_FAULT=after-preferences"})
			if err != nil {
				return err
			}
			if success {
				return errors.New("fault-not-triggered")
			}
			_, success, err = fault.artifactProbeRun(ctx, first, arguments, nil)
			if err != nil {
				return err
			}
			if success {
				return errors.New("pending-setup-not-refused")
			}
			result, err := fault.artifactProbeAction(ctx, first, "recover", true)
			if err != nil {
				return err
			}
			if artifactProbeMap(result["status"])["installed"] != false {
				return errors.New("recovery-active-outcome")
			}
			if err := fault.artifactProbePreserved(true); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(fault.config, "preferences.json")); !os.IsNotExist(err) {
				return errors.New("recovery-preferences")
			}
			if _, err := fault.artifactProbeAction(ctx, first, "install", true, "--host", "all", "--shell", "none", "--preset", "recommended"); err != nil {
				return err
			}
			if err := fault.artifactProbeVerify(identities[0], identities[0], false); err != nil {
				return err
			}
			result, err = fault.artifactProbeAction(ctx, first, "uninstall", true)
			if err != nil {
				return err
			}
			if artifactProbeMap(result["status"])["installed"] != false {
				return errors.New("fault-uninstall-outcome")
			}
			if err := fault.artifactProbePreserved(true); err != nil {
				return err
			}
			keys, err := os.ReadFile(filepath.Join(home.config, "credentials.json"))
			if err != nil {
				return errors.New("keys-read")
			}
			result, err = home.artifactProbeAction(ctx, installed, "uninstall", false)
			if err != nil {
				return err
			}
			if artifactProbeMap(result["status"])["installed"] != false {
				return errors.New("uninstall-outcome")
			}
			if err := home.artifactProbePreserved(true); err != nil {
				return err
			}
			saved, readErr := os.ReadFile(filepath.Join(home.config, "preferences.json"))
			kept, keyErr := os.ReadFile(filepath.Join(home.config, "credentials.json"))
			info, statErr := os.Stat(filepath.Join(home.config, "credentials.json"))
			if readErr != nil || keyErr != nil || statErr != nil || info.Mode().Perm() != 0600 || !bytes.Equal(saved, preferences) || !bytes.Equal(kept, keys) {
				return errors.New("uninstall-preferences-keys")
			}
			return lab.artifactProbeAudit(false)
		}},
		{"R-A4", func() error {
			if !lab.verified {
				return errors.New("https-not-exercised")
			}
			return lab.artifactProbeAudit(false)
		}},
	}
	completed := []string{}
	for _, testCase := range cases {
		if ctx.Err() != nil {
			return report, fmt.Errorf("%s/probe-deadline", testCase.id)
		}
		if err := testCase.run(); err != nil {
			return report, fmt.Errorf("%s/%s", testCase.id, err.Error())
		}
		completed = append(completed, testCase.id)
		report["cases"] = completed
	}
	report["status"] = "success"
	artifactProbeMap(report["coverage"])["native_https_asset"] = true
	return report, nil
}
