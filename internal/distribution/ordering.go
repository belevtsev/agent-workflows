// Package distribution acquires published standalone managers. It never changes
// installation ownership, host settings, command pointers, or source checkouts.
package distribution

import (
	"errors"
	"regexp"
	"strings"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var sourceRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)
var contentDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func compareVersion(first, second string) (int, error) {
	left, right := stableVersion.FindStringSubmatch(first), stableVersion.FindStringSubmatch(second)
	if len(left) != 4 || len(right) != 4 || len(first) > 128 || len(second) > 128 {
		return 0, errors.New("update authority requires a published stable semantic version")
	}
	for index := 1; index < 4; index++ {
		if len(left[index]) < len(right[index]) {
			return -1, nil
		}
		if len(left[index]) > len(right[index]) {
			return 1, nil
		}
		if order := strings.Compare(left[index], right[index]); order != 0 {
			return order, nil
		}
	}
	return 0, nil
}

// CheckOrder compares with the highest capable installed manager, including
// retained managers after rollback. An empty highWater denotes a fresh install.
func CheckOrder(target, highWater bundled.Identity) error {
	if _, err := bundled.BinaryMarker(target); err != nil {
		return err
	}
	if _, err := compareVersion(target.Version, target.Version); err != nil {
		return err
	}
	if highWater == (bundled.Identity{}) {
		return nil
	}
	if _, err := bundled.BinaryMarker(highWater); err != nil {
		return errors.New("installed manager has an invalid high-water identity")
	}
	if target.OS != highWater.OS || target.Arch != highWater.Arch {
		return errors.New("target release differs from installed manager platform")
	}
	order, err := compareVersion(target.Version, highWater.Version)
	if err != nil {
		return err
	}
	if order < 0 {
		return errors.New("target release is older than the highest capable installed manager")
	}
	if order == 0 && target != highWater {
		return errors.New("same-version release conflicts with installed source or bundle identity")
	}
	return nil
}
