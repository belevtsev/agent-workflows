// Implementation. The test engineer must never read this file.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "init" {
		fmt.Fprintln(os.Stderr, "notes: usage: notes init [--dir PATH] [--json]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target directory")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	created, path, err := initialize(*dir)
	if err != nil {
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
		}
		fmt.Fprintln(os.Stderr, "notes: "+err.Error())
		os.Exit(2)
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"created": created, "path": path})
		return
	}
	fmt.Println("initialized " + path)
}

func initialize(dir string) (bool, string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false, "", err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return false, "", errors.New("directory does not exist: " + abs)
	}
	target := filepath.Join(abs, ".notes")
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, "", errors.New("refusing to use " + target + ": not a directory")
		}
		if _, err := os.Stat(filepath.Join(target, "config.json")); err == nil {
			return false, target, nil
		}
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return false, "", err
	}
	if err := os.WriteFile(filepath.Join(target, "config.json"), []byte("{\"version\":1}\n"), 0o600); err != nil {
		return false, "", err
	}
	return true, target, nil
}
