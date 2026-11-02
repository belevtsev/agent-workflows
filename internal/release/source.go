package release

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1" // Git's 40-character object IDs use its native SHA-1 blob format.
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxSourceManifest = 4 << 20
const maxSourceFiles = 10000
const maxSourceBytes = 128 << 20

type sourceFile struct {
	name   string
	object string
	mode   fs.FileMode
}

// exportSource copies only the exact commit's regular blobs into a private
// directory. Reading objects directly avoids worktree edits, checkout filters,
// and archive attributes that can omit or rewrite build inputs and notices.
func exportSource(ctx context.Context, source, revision, destination string) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	command := sourceCommand(ctx, source, "ls-tree", "-rz", "--full-tree", revision)
	var manifest sourceBuffer
	manifest.limit = maxSourceManifest
	command.Stdout = &manifest
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("list exact release source: %w", err)
	}
	files, err := sourceFiles(manifest.Bytes())
	if err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(destination))
		}
	}()
	var queries strings.Builder
	for _, file := range files {
		queries.WriteString(file.object + "\n")
	}
	command = sourceCommand(ctx, source, "cat-file", "--batch")
	command.Stdin = strings.NewReader(queries.String())
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	// Closing the pipe also interrupts a read if a descendant retains stdout
	// after the context has stopped Git itself.
	stop := context.AfterFunc(ctx, func() { output.Close() })
	defer stop()
	copyErr := extractSourceBlobs(ctx, output, files, destination)
	if copyErr != nil {
		cancel()
	}
	waitErr := command.Wait()
	if copyErr != nil {
		return copyErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("read exact release source: %w", waitErr)
	}
	return nil
}

func sourceCommand(ctx context.Context, source string, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", source}, arguments...)...)
	command.Env = environment(map[string]string{"GIT_OPTIONAL_LOCKS": "0", "GIT_TERMINAL_PROMPT": "0", "GIT_NO_REPLACE_OBJECTS": "1"})
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	return command
}

type sourceBuffer struct {
	data  bytes.Buffer
	limit int
}

func (buffer *sourceBuffer) Write(data []byte) (int, error) {
	if len(data) > buffer.limit-buffer.data.Len() {
		return 0, errors.New("release source manifest exceeds input limit")
	}
	return buffer.data.Write(data)
}

func (buffer *sourceBuffer) Bytes() []byte {
	return buffer.data.Bytes()
}

func sourceFiles(manifest []byte) ([]sourceFile, error) {
	if len(manifest) == 0 || len(manifest) > maxSourceManifest || manifest[len(manifest)-1] != 0 {
		return nil, errors.New("release source manifest is empty, incomplete or oversized")
	}
	files := make([]sourceFile, 0)
	seen := make(map[string]bool)
	for entry := range bytes.SplitSeq(manifest[:len(manifest)-1], []byte{0}) {
		metadata, filename, ok := bytes.Cut(entry, []byte{'\t'})
		fields := strings.Split(string(metadata), " ")
		name := string(filename)
		if !ok || len(fields) != 3 || !revisionPattern.MatchString(fields[2]) {
			return nil, errors.New("release source manifest contains an invalid object")
		}
		if name == "" || path.Clean(name) != name || !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, "\\\x00\r\n") || seen[name] {
			return nil, errors.New("release source contains an unsafe or duplicate path")
		}
		for component := range strings.SplitSeq(name, "/") {
			if strings.EqualFold(component, ".git") {
				return nil, errors.New("release source contains a reserved Git path")
			}
		}
		if fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return nil, errors.New("release source contains a link or unsupported entry")
		}
		mode := fs.FileMode(0o644)
		if fields[0] == "100755" {
			mode = 0o755
		}
		files = append(files, sourceFile{name: name, object: fields[2], mode: mode})
		seen[name] = true
		if len(files) > maxSourceFiles {
			return nil, errors.New("release source exceeds file count limit")
		}
	}
	return files, nil
}

func extractSourceBlobs(ctx context.Context, input io.Reader, files []sourceFile, destination string) error {
	reader := bufio.NewReader(input)
	var total int64
	for _, file := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := reader.ReadSlice('\n')
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read release source blob header: %w", err)
		}
		fields := strings.Split(strings.TrimSuffix(string(line), "\n"), " ")
		if len(fields) != 3 || fields[0] != file.object || fields[1] != "blob" {
			return errors.New("release source blob is missing or differs from requested object")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 || size > maxSourceBytes-total {
			return errors.New("release source exceeds blob input limit")
		}
		total += size
		filename := filepath.Join(destination, filepath.FromSlash(file.name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			return err
		}
		output, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.mode)
		if err != nil {
			return err
		}
		digest := sha1.New()
		fmt.Fprintf(digest, "blob %d\x00", size)
		_, copyErr := io.CopyN(io.MultiWriter(output, digest), reader, size)
		if err := errors.Join(copyErr, output.Close()); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if hex.EncodeToString(digest.Sum(nil)) != file.object {
			return errors.New("release source contents differ from Git blob identity")
		}
		if separator, err := reader.ReadByte(); err != nil || separator != '\n' {
			return errors.New("release source blob is incomplete")
		}
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("release source has unexpected trailing output")
	}
	return nil
}
