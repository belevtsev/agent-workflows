//go:build !darwin && !linux

package devcheck

import "io/fs"

// Without flock, copies are only removed by their own run.
func lockSnapshot(string) (func(), error) { return func() {}, nil }

func tryLockSnapshot(string) (func(), bool, bool) { return nil, false, false }

func ownedByCurrentUser(fs.FileInfo) bool { return false }
