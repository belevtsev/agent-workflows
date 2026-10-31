package workflow

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"unicode/utf16"
)

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func object(v any) Object { x, _ := v.(map[string]any); return x }
func text(v any) string   { x, _ := v.(string); return x }
func integer(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case int64:
		return int(x)
	}
	return 0
}
func sequence(v any) []any  { x, _ := v.([]any); return x }
func clone(v Object) Object { return maps.Clone(v) }
func equal(a, b any) bool {
	aa, _ := json.Marshal(a, json.Deterministic(true))
	bb, _ := json.Marshal(b, json.Deterministic(true))
	return bytes.Equal(aa, bb)
}
func hash(data []byte) string   { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func encode(data []byte) string { return base64.StdEncoding.EncodeToString(data) }
func decode(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, errors.New("corrupt encoded local state")
	}
	if strings.ContainsAny(s, "\r\n") {
		return nil, errors.New("corrupt encoded local state")
	}
	return base64.StdEncoding.Strict().DecodeString(s)
}

// Version-one receipts were sealed with Python's sorted, indented, ASCII JSON.
func legacyJSON(v any) []byte {
	data, err := json.Marshal(v, json.Deterministic(true), jsontext.WithIndent("  "), jsontext.EscapeForHTML(false), jsontext.EscapeForJS(false))
	if err != nil {
		panic(err)
	}
	var out strings.Builder
	for _, r := range string(data) {
		if r < 128 {
			out.WriteRune(r)
		} else if r <= 0xffff {
			fmt.Fprintf(&out, `\u%04x`, r)
		} else {
			a, b := utf16.EncodeRune(r)
			fmt.Fprintf(&out, `\u%04x\u%04x`, a, b)
		}
	}
	return append([]byte(out.String()), '\n')
}
func seal(v Object) Object {
	x := clone(v)
	delete(x, "integrity_sha256")
	x["integrity_sha256"] = hash(legacyJSON(x))
	return x
}
func verifySeal(v Object) error {
	if v == nil || v["integrity_sha256"] != seal(v)["integrity_sha256"] {
		return errors.New("corrupt local state integrity checksum")
	}
	return nil
}
func readJSON(path string) (Object, error) {
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe local state: %s", path)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var v Object
	e = json.Unmarshal(data, &v)
	return v, e
}
func unixMode(mode os.FileMode) int {
	value := int(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		value |= 04000
	}
	if mode&os.ModeSetgid != 0 {
		value |= 02000
	}
	if mode&os.ModeSticky != 0 {
		value |= 01000
	}
	return value
}
func fileMode(v any) os.FileMode {
	n := integer(v)
	mode := os.FileMode(n & 0777)
	if n&04000 != 0 {
		mode |= os.ModeSetuid
	}
	if n&02000 != 0 {
		mode |= os.ModeSetgid
	}
	if n&01000 != 0 {
		mode |= os.ModeSticky
	}
	return mode
}
func Normalize(path string) string { value, _ := filepath.Abs(path); return normalizeAlias(value) }
func normalizeAlias(path string) string {
	for _, name := range []string{"var", "tmp"} {
		alias := "/" + name
		target, e := os.Readlink(alias)
		if e == nil && (target == "private/"+name || target == "/private/"+name) && (path == alias || strings.HasPrefix(path, alias+"/")) {
			return "/private" + path
		}
	}
	return path
}
func realDirectory(path string, create bool) error {
	path = Normalize(path)
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e == nil {
			if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe directory: %s", p)
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if create {
		return os.MkdirAll(path, 0700)
	}
	return nil
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// durableDirectory publishes every new ancestor before callers can move the
// only copy of owned content below it. Syncing the final child alone does not
// persist its entry in a newly created parent directory.
func durableDirectory(path string, syncDirectory func(string) error) error {
	if err := realDirectory(path, false); err != nil {
		return err
	}
	var directories []string
	ancestor := path
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || ancestor == filepath.Dir(ancestor) {
			return err
		}
		directories = append(directories, ancestor)
		ancestor = filepath.Dir(ancestor)
	}
	if err := realDirectory(path, true); err != nil {
		return err
	}
	// Child metadata, each new parent's child entries, and the existing
	// ancestor's entry are flushed before a destructive source rename.
	directories = append(directories, ancestor)
	if parent := filepath.Dir(ancestor); parent != ancestor {
		directories = append(directories, parent)
	}
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if e := realDirectory(filepath.Dir(path), true); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".agent-workflows-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}
func inventory(root string) (Object, error) {
	out := Object{}
	e := filepath.WalkDir(root, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == root {
			return nil
		}
		relative, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		st, e := os.Lstat(path)
		if e != nil {
			return e
		}
		item := Object{"mode": unixMode(st.Mode())}
		switch {
		case st.Mode()&os.ModeSymlink != 0:
			target, e := os.Readlink(path)
			if e != nil {
				return e
			}
			item = Object{"kind": "symlink", "target": target}
		case st.IsDir():
			item["kind"] = "directory"
		case st.Mode().IsRegular():
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			item["kind"] = "file"
			item["sha256"] = hash(data)
		default:
			return fmt.Errorf("unsupported inventory path: %s", path)
		}
		out[filepath.ToSlash(relative)] = item
		return nil
	})
	return out, e
}
func observe(path string) (Object, error) {
	st, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return Object{"kind": "absent"}, nil
	}
	if e != nil {
		return nil, e
	}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		target, e := os.Readlink(path)
		return Object{"kind": "symlink", "target": target}, e
	case st.Mode().IsRegular():
		data, e := os.ReadFile(path)
		return Object{"kind": "file", "data": encode(data), "mode": unixMode(st.Mode())}, e
	case st.IsDir():
		contents, e := inventory(path)
		return Object{"kind": "directory", "inventory": contents, "mode": unixMode(st.Mode())}, e
	}
	return nil, fmt.Errorf("unsupported occupied path: %s", path)
}
func writeObservation(path string, v Object) error {
	if err := realDirectory(filepath.Dir(path), false); err != nil {
		return err
	}
	switch text(v["kind"]) {
	case "absent":
		st, e := os.Lstat(path)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if st.IsDir() {
			return fmt.Errorf("refusing directory removal: %s", path)
		}
		if e = os.Remove(path); e != nil {
			return e
		}
		return syncDir(filepath.Dir(path))
	case "file":
		data, e := decode(v["data"])
		if e != nil {
			return e
		}
		return atomicWrite(path, data, fileMode(v["mode"]))
	case "symlink":
		if e := realDirectory(filepath.Dir(path), true); e != nil {
			return e
		}
		f, e := os.CreateTemp(filepath.Dir(path), ".agent-workflows-link-")
		if e != nil {
			return e
		}
		name := f.Name()
		f.Close()
		os.Remove(name)
		defer os.Remove(name)
		if e = os.Symlink(text(v["target"]), name); e != nil {
			return e
		}
		if e = os.Rename(name, path); e != nil {
			return e
		}
		return syncDir(filepath.Dir(path))
	}
	return errors.New("unsupported write kind")
}
func git(source string, args ...string) ([]byte, error) {
	return gitContext(context.Background(), source, args...)
}
func gitContext(ctx context.Context, source string, args ...string) ([]byte, error) {
	argv := append([]string{"-c", "gc.auto=0", "-c", "maintenance.auto=false", "-C", source}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, e := cmd.Output()
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
func fault(label string) error {
	if os.Getenv("AGENT_WORKFLOWS_FAULT") == label {
		return fmt.Errorf("injected failure at %s (recover before retrying)", label)
	}
	return nil
}
func cleanHead(source string) (string, error) {
	return cleanHeadContext(context.Background(), source)
}
func cleanHeadContext(ctx context.Context, source string) (string, error) {
	out, e := gitContext(ctx, source, "status", "--porcelain", "--untracked-files=all")
	if e != nil {
		return "", e
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return "", errors.New("source checkout is dirty or has untracked files")
	}
	out, e = gitContext(ctx, source, "rev-parse", "HEAD")
	sha := strings.TrimSpace(string(out))
	if e != nil {
		return "", e
	}
	if !commitSHA.MatchString(sha) {
		return "", errors.New("source HEAD is not a full commit SHA")
	}
	return sha, nil
}
func descendant(source, before, after string) error {
	return descendantContext(context.Background(), source, before, after)
}
func descendantContext(ctx context.Context, source, before, after string) error {
	_, e := gitContext(ctx, source, "merge-base", "--is-ancestor", before, after)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("update is diverged or rewinds history; only a clean fast-forward is allowed")
	}
	return nil
}
func exportCommit(source, sha, directory string) (Object, error) {
	return exportCommitContext(context.Background(), source, sha, directory)
}
func exportCommitContext(ctx context.Context, source, sha, directory string) (Object, error) {
	data, e := gitContext(ctx, source, "-c", "tar.umask=0022", "archive", "--format=tar", sha)
	if e != nil {
		return nil, e
	}
	reader := tar.NewReader(bytes.NewReader(data))
	dirs := map[string]os.FileMode{}
	for {
		h, e := reader.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
		// git archive includes its commit ID in a global PAX metadata header.
		// This header describes the archive and is never a filesystem entry.
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) {
			return nil, fmt.Errorf("unsafe archive entry: %s", h.Name)
		}
		path := filepath.Join(directory, name)
		if h.Typeflag == tar.TypeDir {
			if e = os.MkdirAll(path, 0700); e != nil {
				return nil, e
			}
			dirs[path] = os.FileMode(h.Mode) & 0777
			continue
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return nil, e
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		_, e = io.Copy(f, reader)
		ce := f.Close()
		if e != nil {
			return nil, e
		}
		if ce != nil {
			return nil, ce
		}
		if e = os.Chmod(path, os.FileMode(h.Mode)&0777); e != nil {
			return nil, e
		}
	}
	for p, mode := range dirs {
		if e = os.Chmod(p, mode); e != nil {
			return nil, e
		}
	}
	return LoadManifestValidated(directory)
}
func LoadManifestValidated(root string) (Object, error) {
	if _, e := ValidateSuite(root); e != nil {
		return nil, fmt.Errorf("suite validation failed: %w", e)
	}
	return LoadManifest(root)
}
func keys(v Object) []string  { return slices.Sorted(maps.Keys(v)) }
func exists(path string) bool { _, e := os.Lstat(path); return e == nil }
func lockRoot(root string, recovery bool) (func(), error) {
	if e := durableDirectory(root, syncDir); e != nil {
		return nil, e
	}
	if e := os.Chmod(root, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(root, "mutation.lock")
	if st, e := os.Lstat(path); e == nil && !st.Mode().IsRegular() {
		return nil, errors.New("unsafe mutation lock")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("another installer mutation holds the lock")
	}
	if exists(filepath.Join(root, "journal.json")) && !recovery {
		f.Close()
		return nil, errors.New("an unfinished mutation requires recover")
	}
	return func() { f.Close() }, nil
}
func assertEqual(a, b any, message string) error {
	if !equal(a, b) {
		return errors.New(message)
	}
	return nil
}
func requiredObject(v any, description string) (Object, error) {
	x := object(v)
	if x == nil {
		return nil, fmt.Errorf("corrupt %s", description)
	}
	return x, nil
}

func validMode(value any) bool {
	switch v := value.(type) {
	case int:
		return v >= 0 && v <= 07777
	case int64:
		return v >= 0 && v <= 07777
	case float64:
		return v >= 0 && v <= 07777 && v == float64(int(v))
	}
	return false
}
func validateObservation(value Object, directory bool) error {
	if value == nil {
		return errors.New("corrupt path observation")
	}
	switch value["kind"] {
	case "absent":
		if len(value) != 1 {
			return errors.New("corrupt absent observation")
		}
	case "symlink":
		if len(value) != 2 || text(value["target"]) == "" || strings.IndexByte(text(value["target"]), 0) >= 0 {
			return errors.New("corrupt symlink observation")
		}
	case "file":
		if len(value) != 3 || !validMode(value["mode"]) {
			return errors.New("corrupt file observation mode")
		}
		if _, err := decode(value["data"]); err != nil {
			return err
		}
	case "directory":
		if !directory || len(value) != 3 || !validMode(value["mode"]) || object(value["inventory"]) == nil {
			return errors.New("corrupt directory observation")
		}
		for name, raw := range object(value["inventory"]) {
			if filepath.IsAbs(name) || name == "." || name == ".." || filepath.Clean(name) != name || strings.HasPrefix(name, "../") {
				return errors.New("unsafe inventory path")
			}
			item := object(raw)
			switch item["kind"] {
			case "file":
				if len(item) != 3 || !validMode(item["mode"]) || len(text(item["sha256"])) != 64 {
					return errors.New("corrupt inventory file")
				}
				if _, err := hex.DecodeString(text(item["sha256"])); err != nil {
					return err
				}
			case "directory":
				if len(item) != 2 || !validMode(item["mode"]) {
					return errors.New("corrupt inventory directory")
				}
			case "symlink":
				if len(item) != 2 || text(item["target"]) == "" || strings.IndexByte(text(item["target"]), 0) >= 0 {
					return errors.New("corrupt inventory symlink")
				}
			default:
				return errors.New("unsupported inventory kind")
			}
		}
	default:
		return errors.New("unsupported observation kind")
	}
	return nil
}
