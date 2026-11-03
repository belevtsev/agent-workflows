//go:build !darwin && !linux

package graphify

import "context"

func acquireCacheLock(context.Context, string) (func(), error) {
	return nil, refuse("platform", "The guarded Graphify cache currently supports macOS and Linux")
}
