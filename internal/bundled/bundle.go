package bundled

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

const maxPackBytes = 64 << 20
const maxContentBytes = 128 << 20
const maxFiles = 10000
const metadataName = ".cw-bundle.json"

type packFile struct {
	Path   string `json:"path"`
	Mode   int64  `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type packMetadata struct {
	Format   int        `json:"format"`
	Version  string     `json:"version"`
	Revision string     `json:"revision"`
	Files    []packFile `json:"files"`
}

// Bundle contains only validated, immutable data. ReadFile returns a copy.
type Bundle struct {
	identity Identity
	index    []packFile
	files    map[string][]byte
}

func payloadDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safeName(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !path.IsAbs(name) &&
		!strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00\r\n:")
}

// Open validates the complete pack, including trailers, the exact inventory and
// each file digest. It does not create files or invoke external programs.
func Open(data []byte) (*Bundle, error) {
	if len(data) == 0 || len(data) > maxPackBytes {
		return nil, errors.New("bundle exceeds compressed input limit or is empty")
	}
	compressed := bytes.NewReader(data)
	zipped, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("invalid bundle compression: %w", err)
	}
	defer zipped.Close()
	zipped.Multistream(false)
	limited := &io.LimitedReader{R: zipped, N: maxContentBytes + (8 << 20)}
	reader := tar.NewReader(limited)
	header, err := reader.Next()
	if err != nil || header.Name != metadataName || header.Typeflag != tar.TypeReg || header.Mode != 0o644 || header.Size <= 0 || header.Size > 4<<20 {
		return nil, errors.New("bundle has no bounded regular metadata header")
	}
	metadataBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var metadata packMetadata
	if err := json.Unmarshal(metadataBytes, &metadata, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("invalid bundle inventory: %w", err)
	}
	if metadata.Format != 1 || !versionPattern.MatchString(metadata.Version) || !revisionPattern.MatchString(metadata.Revision) || len(metadata.Files) == 0 || len(metadata.Files) > maxFiles {
		return nil, errors.New("unsupported or incomplete bundle inventory")
	}
	bundle := &Bundle{identity: Identity{Version: metadata.Version, Revision: metadata.Revision, OS: runtime.GOOS, Arch: runtime.GOARCH, Digest: payloadDigest(data)}, index: metadata.Files, files: make(map[string][]byte, len(metadata.Files))}
	var total int64
	previous := ""
	for _, file := range metadata.Files {
		if !safeName(file.Path) || file.Path <= previous || excluded(file.Path) || file.Size < 0 || file.Size > maxContentBytes-total || (file.Mode != 0o644 && file.Mode != 0o755) || !digestPattern.MatchString(file.SHA256) {
			return nil, errors.New("bundle inventory contains an unsafe, duplicate or oversized file")
		}
		for parent := path.Dir(file.Path); parent != "."; parent = path.Dir(parent) {
			if _, exists := bundle.files[parent]; exists {
				return nil, errors.New("bundle file conflicts with a resource directory")
			}
		}
		header, err = reader.Next()
		if err != nil || header.Name != file.Path || header.Typeflag != tar.TypeReg || header.Size != file.Size || header.Mode != file.Mode {
			return nil, errors.New("bundle archive differs from its exact file inventory")
		}
		contents, err := io.ReadAll(reader)
		if err != nil || int64(len(contents)) != file.Size || payloadDigest(contents) != file.SHA256 {
			return nil, errors.New("bundle resource differs from its file digest")
		}
		bundle.files[file.Path] = contents
		total += file.Size
		previous = file.Path
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		return nil, errors.New("bundle contains unlisted archive entries")
	}
	trailing, err := io.ReadAll(limited)
	if err != nil || limited.N <= 0 || compressed.Len() != 0 || len(bytes.Trim(trailing, "\x00")) != 0 {
		return nil, errors.New("bundle contains corrupt or trailing compressed data")
	}
	manifest, err := decodeManifest(bundle.files["skills-manifest.json"])
	if err != nil {
		return nil, err
	}
	for name := range bundle.files {
		if !manifest.allows(name) {
			return nil, fmt.Errorf("bundle contains an unregistered resource: %s", name)
		}
	}
	for _, name := range manifest.requiredFiles() {
		if _, ok := bundle.files[name]; !ok {
			return nil, fmt.Errorf("bundle is missing required resource: %s", name)
		}
	}
	for _, root := range manifest.Registrations {
		found := false
		for name := range bundle.files {
			if strings.HasPrefix(name, root+"/") && path.Base(name) == "SKILL.md" {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("bundle registration has no skill entrypoint: %s", root)
		}
	}
	return bundle, nil
}

func (bundle *Bundle) Identity() Identity { return bundle.identity }

// ReadFile returns a defensive copy of an indexed resource.
func (bundle *Bundle) ReadFile(name string) ([]byte, error) {
	data, ok := bundle.files[name]
	if !ok || !safeName(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	return bytes.Clone(data), nil
}

// Extract creates a new destination through physical, nofollow directory
// descriptors. Every write stays relative to that owned tree, including cleanup.
func (bundle *Bundle) Extract(destination string) (err error) {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination || destination == "/" || strings.ContainsAny(destination, "\x00\r\n") {
		return errors.New("bundle extraction requires an absolute clean destination")
	}
	parent, err := physicalDirectory(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(destination)
	if err = unix.Mkdirat(int(parent.Fd()), name, 0700); err != nil {
		return err
	}
	descriptor, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	root := os.NewFile(uintptr(descriptor), "bundle-destination")
	defer root.Close()
	defer func() {
		if err == nil {
			return
		}
		var opened, named unix.Stat_t
		if unix.Fstat(int(root.Fd()), &opened) != nil || unix.Fstatat(int(parent.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || opened.Dev != named.Dev || opened.Ino != named.Ino {
			err = errors.Join(err, errors.New("extraction destination changed; cleanup refused"))
			return
		}
		cleanupErr := removeExtracted(root)
		if cleanupErr == nil {
			cleanupErr = unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
		}
		err = errors.Join(err, cleanupErr)
	}()
	directories := map[string]bool{".": true}
	for _, file := range bundle.index {
		directory, e := relativeDirectory(root, path.Dir(file.Path), true)
		if e != nil {
			return e
		}
		fd, e := unix.Openat(int(directory.Fd()), path.Base(file.Path), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(file.Mode))
		if e != nil {
			directory.Close()
			return e
		}
		output := os.NewFile(uintptr(fd), "bundle-resource")
		_, writeErr := output.Write(bundle.files[file.Path])
		e = errors.Join(writeErr, output.Chmod(fs.FileMode(file.Mode)), output.Sync(), output.Close(), directory.Close())
		if e != nil {
			return e
		}
		for current := path.Dir(file.Path); current != "."; current = path.Dir(current) {
			directories[current] = true
		}
	}
	names := make([]string, 0, len(directories))
	for name := range directories {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(b, a) })
	for _, name := range names {
		directory, e := relativeDirectory(root, name, false)
		if e != nil {
			return e
		}
		e = errors.Join(directory.Chmod(0755), directory.Sync(), directory.Close())
		if e != nil {
			return e
		}
	}
	return parent.Sync()
}

func physicalDirectory(name string) (*os.File, error) {
	descriptor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	root := os.NewFile(uintptr(descriptor), "bundle-physical-root")
	defer root.Close()
	return relativeDirectory(root, strings.TrimPrefix(name, "/"), false)
}

func relativeDirectory(root *os.File, name string, create bool) (*os.File, error) {
	descriptor, err := unix.FcntlInt(root.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			unix.Close(descriptor)
			return nil, errors.New("unsafe extraction directory")
		}
		if create {
			if err = unix.Mkdirat(descriptor, component, 0755); err != nil && !errors.Is(err, unix.EEXIST) {
				unix.Close(descriptor)
				return nil, err
			}
		}
		next, e := unix.Openat(descriptor, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(descriptor)
		if e != nil {
			return nil, e
		}
		descriptor = next
	}
	return os.NewFile(uintptr(descriptor), "bundle-directory"), nil
}

func removeExtracted(directory *os.File) error {
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			child, e := relativeDirectory(directory, entry.Name(), false)
			if e != nil {
				return e
			}
			e = errors.Join(removeExtracted(child), child.Close())
			if e != nil {
				return e
			}
			if e = unix.Unlinkat(int(directory.Fd()), entry.Name(), unix.AT_REMOVEDIR); e != nil {
				return e
			}
		} else if e := unix.Unlinkat(int(directory.Fd()), entry.Name(), 0); e != nil {
			return e
		}
	}
	return nil
}
