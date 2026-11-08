//go:build unix && !linux

package main

// processGroupRunning reports whether the process group still has a member.
// The caller has already seen kill(-group, 0) succeed, and these systems give
// no portable way to tell a zombie from a running process, so a group with a
// member always counts as running.
func processGroupRunning(int) bool { return true }
