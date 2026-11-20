//go:build !windows

package workflowrunner

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const registryLimit = 1 << 20

func readRegistration() ([]byte, error) {
	const name = "workflow-runners.json"
	const directory = "/etc/kitsu"
	location, err := filepath.EvalSymlinks(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("configure a trusted workflow runner in /etc/kitsu/workflow-runners.json")
		}
		return nil, err
	}
	for path := location; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || !administratorOwned(info) {
			return nil, errors.New("workflow runner directory must be administrator controlled")
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	root, err := os.OpenRoot(location)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || !administratorOwned(before) {
		return nil, errors.New("workflow runner registration must be a regular administrator-owned file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !administratorOwned(opened) {
		return nil, errors.New("workflow runner registration changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, registryLimit+1))
	if len(data) > registryLimit {
		return nil, errors.New("workflow runner registration is too large")
	}
	return data, err
}

func administratorOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode().Perm()&0o022 == 0
}
