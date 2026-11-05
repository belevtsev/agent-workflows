// Package bundled provides the immutable resource payload carried by cw.
// It has no dependency on the installer, release publisher, Git, or Go tools.
package bundled

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
)

// Identity binds the pristine payload to a source version and executable target.
// Digest is the lowercase SHA256 of the complete deterministic compressed pack.
type Identity struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Digest   string `json:"base_digest"`
}

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
var binaryMarkerPattern = regexp.MustCompile(`CWBUNDLE1\|([A-Za-z0-9][A-Za-z0-9._+-]{0,127})\|([0-9a-f]{40})\|(darwin|linux)\|(amd64|arm64)\|([0-9a-f]{64})\|CWBEND`)

func validIdentity(identity Identity) bool {
	return versionPattern.MatchString(identity.Version) && revisionPattern.MatchString(identity.Revision) &&
		digestPattern.MatchString(identity.Digest) && (identity.OS == "darwin" || identity.OS == "linux") &&
		(identity.Arch == "amd64" || identity.Arch == "arm64")
}

// BinaryMarker is separate from the historical CWRELEASE1 marker and version
// JSON. It permits validation of all four targets without executing foreign code.
func BinaryMarker(identity Identity) (string, error) {
	if !validIdentity(identity) {
		return "", errors.New("invalid embedded bundle identity")
	}
	return strings.Join([]string{"CWBUNDLE1", identity.Version, identity.Revision, identity.OS, identity.Arch, identity.Digest, "CWBEND"}, "|"), nil
}

// InspectBinary reads exactly one complete retained bundle marker. This is
// identity metadata; the published archive checksum establishes byte identity.
func InspectBinary(executable []byte) (Identity, error) {
	matches := binaryMarkerPattern.FindAllSubmatch(executable, 2)
	if len(matches) != 1 {
		return Identity{}, errors.New("executable has no unique standalone bundle identity")
	}
	fields := matches[0]
	identity := Identity{Version: string(fields[1]), Revision: string(fields[2]), OS: string(fields[3]), Arch: string(fields[4]), Digest: string(fields[5])}
	marker, err := BinaryMarker(identity)
	if err != nil || bytes.Count(executable, []byte(marker)) != 1 {
		return Identity{}, errors.New("invalid standalone executable identity")
	}
	return identity, nil
}
