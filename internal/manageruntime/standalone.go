package manageruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

var standaloneDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

const standaloneProtocol = "cw-manager-v10"
const maxStandaloneBinary = 128 << 20

// InspectStandalone adds the standalone capability checks to the version
// protocol that Inspect decodes. Static target identity is checked before execution.
func InspectStandalone(ctx context.Context, path string) (Candidate, error) {
	if !strings.HasPrefix(path, "/") {
		return Candidate{}, errors.New("standalone candidate requires an absolute path")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStandaloneBinary || info.Mode().Perm()&0o111 == 0 {
		return Candidate{}, errors.New("standalone candidate must be a bounded regular executable")
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != info.Size() {
		return Candidate{}, errors.Join(errors.New("cannot read stable standalone executable"), err)
	}
	static, err := bundled.InspectBinary(data)
	if err != nil || static.OS != runtime.GOOS || static.Arch != runtime.GOARCH {
		return Candidate{}, errors.Join(errors.New("standalone executable targets another platform or lacks embedded resources"), err)
	}
	build, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil || build.Path != "github.com/belevtsev/agent-workflows/cmd/cw" || build.Main.Path != "github.com/belevtsev/agent-workflows" || build.GoVersion != "go1.27.1" {
		return Candidate{}, errors.New("standalone executable module or Go toolchain differs")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	identity, err := Inspect(ctx, path)
	if err != nil {
		return Candidate{}, err
	}
	protocol, err := standaloneOutput(ctx, path, "--manager-protocol")
	if err != nil || string(bytes.TrimSpace(protocol)) != standaloneProtocol {
		return Candidate{}, errors.Join(errors.New("candidate does not support standalone manager protocol cw-manager-v10"), err)
	}
	encoded, err := standaloneOutput(ctx, path, "--bundle-identity")
	if err != nil {
		return Candidate{}, err
	}
	var carried bundled.Identity
	if err := json.Unmarshal(encoded, &carried, json.RejectUnknownMembers(true)); err != nil {
		return Candidate{}, fmt.Errorf("invalid standalone bundle identity protocol: %w", err)
	}
	if carried != static || carried.Version != identity.Version || carried.Revision != identity.Revision || carried.OS != identity.OS || carried.Arch != identity.Arch {
		return Candidate{}, errors.New("standalone resource, binary and version identities differ")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		return Candidate{}, errors.New("standalone executable changed during inspection")
	}
	sum := sha256.Sum256(data)
	identity.Protocol, identity.Bundle, identity.SHA256 = standaloneProtocol, &carried, hex.EncodeToString(sum[:])
	return identity, nil
}

func standaloneOutput(ctx context.Context, path, flag string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, flag)
	var output boundedBuffer
	output.limit = 4096
	command.Stdout, command.Stderr = &output, io.Discard
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("inspect standalone capability: %w", err)
	}
	return output.Bytes(), nil
}
