package sandboxtest

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ownerLabel = "com.agent-workflows.sandbox-run"

const runtimeDockerfile = `# syntax=docker/dockerfile:1
FROM curlimages/curl:8.23.0@sha256:3fb7e13e5d179a845c97d78204f3f1064d1907037f26a3ebf91710571151fd93 AS runtime
LABEL org.opencontainers.image.source="https://github.com/belevtsev/agent-workflows"
WORKDIR /work
USER 10001:10001
ENTRYPOINT ["/fixtures/cwdev"]`

type backendConfig struct {
	binary  string
	private string
	name    string
	runner  commandRunner
}

func minimalHostEnvironment(home string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + filepath.Join(home, "tmp"), "LC_ALL=C"}
}

func (b *backendConfig) prepareDocker(ctx context.Context, report *Report) error {
	// Resolve only the local endpoint using the caller's selected context. All
	// subsequent Docker operations use a private, empty registry configuration.
	contextEnv := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG"} {
		if value := os.Getenv(key); value != "" {
			contextEnv = append(contextEnv, key+"="+value)
		}
	}
	contextCLI := commandRunner{binary: b.binary, env: contextEnv, dir: b.private}
	data, err := contextCLI.run(ctx, 15*time.Second, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if err != nil {
		report.Status = "unavailable"
		report.Limitations = append(report.Limitations, "selected Docker context is unavailable")
		return safeStage("Docker context resolution", err)
	}
	endpoint := strings.TrimSpace(string(data))
	if explicit := os.Getenv("DOCKER_HOST"); explicit != "" {
		endpoint = explicit
	}
	if !strings.HasPrefix(endpoint, "unix:///") || strings.ContainsAny(endpoint, "\x00\r\n") {
		report.Status = "unavailable"
		report.Limitations = append(report.Limitations, "artifact runner requires a local Unix Docker engine endpoint")
		return errors.New("remote Docker endpoints are unsupported for artifact isolation")
	}
	home := filepath.Join(b.private, "docker-home")
	config := filepath.Join(home, ".docker")
	if err := os.MkdirAll(config, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(home, "tmp"), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(config, "config.json"), []byte("{\"auths\":{}}\n"), 0600); err != nil {
		return err
	}
	b.runner = commandRunner{binary: b.binary, env: append(minimalHostEnvironment(home), "DOCKER_CONFIG="+config, "DOCKER_HOST="+endpoint, "DOCKER_CLI_HINTS=false"), dir: b.private}
	data, err = b.runner.run(ctx, 15*time.Second, "version", "--format", "{{json .Server}}")
	if err != nil {
		report.Status = "unavailable"
		report.Limitations = append(report.Limitations, "local Docker engine is unreachable")
		return safeStage("Docker engine availability", err)
	}
	var server struct {
		OS   string `json:"Os"`
		Arch string `json:"Arch"`
	}
	if err := json.Unmarshal(data, &server); err != nil || server.OS != "linux" || (server.Arch != "arm64" && server.Arch != "amd64") {
		report.Status = "unavailable"
		return errors.New("Docker engine must report a supported native Linux platform")
	}
	report.Available, report.OS, report.Arch = true, server.OS, server.Arch
	return nil
}

func copyInfrastructureFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<10 {
		return errors.New("sandbox infrastructure input is missing or unsafe")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	switch filepath.Base(source) {
	case "Dockerfile":
		// Never execute a repository-provided RUN, secret mount, or arbitrary
		// frontend while provisioning this artifact-only environment.
		if strings.TrimSpace(string(data)) != runtimeDockerfile {
			return errors.New("sandbox Dockerfile differs from the pinned artifact-only definition")
		}
	case "sbxenv.yaml":
		if err := validateSBXEnvironment(data); err != nil {
			return err
		}
	default:
		return errors.New("unsupported sandbox infrastructure input")
	}
	return os.WriteFile(destination, data, 0600)
}

func validateSBXEnvironment(data []byte) error {
	var definition struct {
		SchemaVersion  string `yaml:"schemaVersion"`
		Agent          string `yaml:"agent"`
		SandboxOptions struct {
			CPUs   int    `yaml:"cpus"`
			Memory string `yaml:"memory"`
		} `yaml:"sandboxOptions"`
		Env map[string]string `yaml:"env"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return errors.New("SBX environment contains unsupported or unsafe fields")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("SBX environment must be one document")
	}
	if definition.SchemaVersion != "1" || definition.Agent != "shell" || definition.SandboxOptions.CPUs != 2 || definition.SandboxOptions.Memory != "2g" || len(definition.Env) != 1 || definition.Env["CW_ARTIFACT_ACCEPTANCE"] != "1" {
		return errors.New("SBX environment differs from the private artifact-only shell contract")
	}
	return nil
}

func (b *backendConfig) runDocker(ctx context.Context, source, artifacts string, report *Report) (err error) {
	image := b.name + ":acceptance"
	container := b.name
	imageIndex, containerIndex := -1, -1
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if containerIndex >= 0 {
			if b.removeDockerOwned(cleanupCtx, "container", container) == nil {
				report.Resources[containerIndex].Removed = true
			} else {
				report.CleanupErrors = append(report.CleanupErrors, "owned Docker container cleanup could not be verified")
			}
		}
		if imageIndex >= 0 {
			if b.removeDockerOwned(cleanupCtx, "image", image) == nil {
				report.Resources[imageIndex].Removed = true
			} else {
				report.CleanupErrors = append(report.CleanupErrors, "owned Docker image cleanup could not be verified")
			}
		}
	}()
	contextDir := filepath.Join(b.private, "docker-context")
	if err = os.Mkdir(contextDir, 0700); err != nil {
		return err
	}
	if err = copyInfrastructureFile(filepath.Join(source, "testing", "sandbox", "Dockerfile"), filepath.Join(contextDir, "Dockerfile")); err != nil {
		return err
	}
	// A private context contains only the Dockerfile. No repository files,
	// credentials, home paths, or Docker socket enter the image or container.
	data, err := b.runner.run(ctx, 15*time.Second, "image", "ls", "--quiet", "--filter", "reference="+image)
	if err != nil {
		return safeStage("Docker target inspection", err)
	}
	if strings.TrimSpace(string(data)) != "" {
		return errors.New("unique Docker image target is already occupied")
	}
	imageIndex = len(report.Resources)
	report.Resources = append(report.Resources, Resource{Kind: "docker-image", Name: image})
	if _, err = b.runner.run(ctx, 3*time.Minute, "build", "--pull", "--label", ownerLabel+"="+b.name, "--tag", image, contextDir); err != nil {
		return safeStage("Docker artifact environment build", err)
	}
	containerIndex = len(report.Resources)
	report.Resources = append(report.Resources, Resource{Kind: "docker-container", Name: container})
	// Staged native managers must execute from these job-private mounts;
	// omitting exec leaves Docker's tmpfs mounts unable to run them.
	args := []string{"create", "--name", container, "--label", ownerLabel + "=" + b.name, "--network", "none", "--add-host", "github.com:127.0.0.1", "--add-host", "api.github.com:127.0.0.1", "--sysctl", "net.ipv4.ip_unprivileged_port_start=0", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "128", "--memory", "1g", "--cpus", "2", "--user", "10001:10001", "--tmpfs", "/tmp:rw,exec,nosuid,nodev,mode=1777,size=64m", "--tmpfs", "/work:rw,exec,nosuid,nodev,mode=1777,size=512m", "--mount", "type=bind,src=" + artifacts + ",dst=/fixtures,readonly", "--env", "HOME=/work/home", "--env", "TMPDIR=/tmp", "--env", "PATH=/usr/local/bin:/usr/bin:/bin", "--workdir", "/work", image, "sandbox", "probe", "--first", "/fixtures/first-cw", "--second", "/fixtures/second-cw", "--root", "/work/synthetic", "--expected-revision", report.SourceRevision}
	if _, err = b.runner.run(ctx, 30*time.Second, args...); err != nil {
		return safeStage("Docker isolated container creation", err)
	}
	data, err = b.runner.run(ctx, 10*time.Minute, "start", "--attach", container)
	if err != nil {
		report.Probe, _ = decodeProbe(data)
		return safeStage("Docker independent acceptance probe", err)
	}
	report.Probe, err = decodeProbe(data)
	return err
}

func (b *backendConfig) removeDockerOwned(ctx context.Context, kind, name string) error {
	inspect := []string{"inspect", "--format", "{{.Id}} {{index .Config.Labels \"" + ownerLabel + "\"}}", name}
	if kind == "image" {
		inspect = append([]string{"image"}, inspect...)
	}
	data, err := b.runner.run(ctx, 10*time.Second, inspect...)
	if err != nil {
		// A failed create/build can leave no named resource. Verify absence with
		// a successful listing rather than treating transport errors as absence.
		args := []string{"container", "ls", "--all", "--quiet", "--filter", "name=^/" + name + "$"}
		if kind == "image" {
			args = []string{"image", "ls", "--quiet", "--filter", "reference=" + name}
		}
		listed, listErr := b.runner.run(ctx, 10*time.Second, args...)
		if listErr == nil && strings.TrimSpace(string(listed)) == "" {
			return nil
		}
		return errors.New("cannot verify owned Docker resource")
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[1] != b.name {
		return errors.New("Docker resource ownership label changed")
	}
	if kind == "image" {
		_, err = b.runner.run(ctx, 10*time.Second, "image", "rm", fields[0])
	} else {
		_, err = b.runner.run(ctx, 10*time.Second, "container", "rm", "--force", fields[0])
	}
	return err
}

func (b *backendConfig) runSBX(ctx context.Context, source, artifacts string, report *Report) (err error) {
	home := filepath.Join(b.private, "sbx-home")
	if err = os.MkdirAll(filepath.Join(home, "tmp"), 0700); err != nil {
		return err
	}
	b.runner = commandRunner{binary: b.binary, env: append(minimalHostEnvironment(home), "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_STATE_HOME="+filepath.Join(home, ".local", "state"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share")), dir: b.private}
	configuration := filepath.Join(b.private, "sbxenv.yaml")
	if err = copyInfrastructureFile(filepath.Join(source, "testing", "sandbox", "sbxenv.yaml"), configuration); err != nil {
		return err
	}
	prefix := []string{"--app-name", b.name, "--no-input"}
	call := func(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
		return b.runner.run(ctx, timeout, append(append([]string{}, prefix...), args...)...)
	}
	attempted := false
	resourceIndex := -1
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if attempted {
			if _, e := call(cleanupCtx, 25*time.Second, "env", "rm", "--force", "--skip-host-commands", "--name", b.name, configuration); e != nil {
				report.CleanupErrors = append(report.CleanupErrors, "private SBX environment cleanup could not be verified")
			} else if resourceIndex >= 0 {
				report.Resources[resourceIndex].Removed = true
			}
		}
		if _, e := call(cleanupCtx, 15*time.Second, "daemon", "stop"); e != nil {
			report.CleanupErrors = append(report.CleanupErrors, "private SBX daemon cleanup could not be verified")
		}
	}()
	if _, err = call(ctx, 45*time.Second, "policy", "init", "deny-all"); err != nil {
		report.Status, report.Available = "unavailable", false
		report.Limitations = append(report.Limitations, "private SBX setup or authentication is unavailable; no login or shared settings were changed")
		return safeStage("SBX availability and deny-all initialization", err)
	}
	attempted = true
	resourceIndex = len(report.Resources)
	report.Resources = append(report.Resources, Resource{Kind: "sbx-environment", Name: b.name})
	if _, err = call(ctx, 3*time.Minute, "env", "create", "--force", "--skip-host-commands", "--name", b.name, configuration); err != nil {
		report.Status, report.Available = "unavailable", false
		return safeStage("private SBX shell creation", err)
	}
	// Use explicit per-sandbox deny even within the isolated application's
	// deny-all policy. Loopback fixtures remain inside the guest.
	if _, err = call(ctx, 20*time.Second, "policy", "deny", "network", "--sandbox", b.name, "**"); err != nil {
		return safeStage("SBX per-sandbox network isolation", err)
	}
	rootSetup := "printf '\\n127.0.0.1 github.com api.github.com\\n' >> /etc/hosts && sysctl -w net.ipv4.ip_unprivileged_port_start=0 >/dev/null && mkdir -p /tmp/cw-fixtures /tmp/cw-run && chown 10001:10001 /tmp/cw-run && chmod 0700 /tmp/cw-run && chmod 1777 /tmp/cw-fixtures"
	if _, err = call(ctx, 20*time.Second, "exec", "--user", "root", b.name, "/usr/bin/env", "-i", "PATH=/usr/sbin:/sbin:/usr/bin:/bin", "/bin/sh", "-c", rootSetup); err != nil {
		return safeStage("SBX private artifact directory creation", err)
	}
	for _, filename := range []string{"first-cw", "second-cw", "cwdev"} {
		if _, err = call(ctx, 30*time.Second, "cp", filepath.Join(artifacts, filename), b.name+":/tmp/cw-fixtures/"+filename); err != nil {
			return safeStage("SBX artifact-only copy", err)
		}
	}
	lockArtifacts := "chown 0:0 /tmp/cw-fixtures /tmp/cw-fixtures/first-cw /tmp/cw-fixtures/second-cw /tmp/cw-fixtures/cwdev && chmod 0555 /tmp/cw-fixtures /tmp/cw-fixtures/first-cw /tmp/cw-fixtures/second-cw /tmp/cw-fixtures/cwdev"
	if _, err = call(ctx, 20*time.Second, "exec", "--user", "root", b.name, "/usr/bin/env", "-i", "PATH=/usr/sbin:/sbin:/usr/bin:/bin", "/bin/sh", "-c", lockArtifacts); err != nil {
		return safeStage("SBX copied artifact permissions", err)
	}
	data, err := call(ctx, 10*time.Minute, "env", "exec", "--user", "10001:10001", "--name", b.name, configuration, "--", "/usr/bin/env", "-i", "HOME=/tmp/cw-run/home", "PATH=/usr/local/bin:/usr/bin:/bin", "TMPDIR=/tmp", "/tmp/cw-fixtures/cwdev", "sandbox", "probe", "--first", "/tmp/cw-fixtures/first-cw", "--second", "/tmp/cw-fixtures/second-cw", "--root", "/tmp/cw-run/synthetic", "--expected-revision", report.SourceRevision)
	if err != nil {
		report.Probe, _ = decodeProbe(data)
		return safeStage("SBX independent acceptance probe", err)
	}
	report.Probe, err = decodeProbe(data)
	return err
}
