package advisers

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/proc"
)

// Execute makes exactly one direct invocation. A command containing
// {answer_file} must create that absent absolute path; stdout is not a fallback.
// Environment and diagnostics are never included in Execution's JSON record.
func Execute(ctx context.Context, root string, definition Definition, prompt, answerFile string) (result Execution) {
	result = Execution{Outcome: "failed", ActualModel: "unknown", StartedAt: time.Now().UTC()}
	defer func() { result.EndedAt = time.Now().UTC() }()
	if definition.Timeout == 0 {
		definition.Timeout = DefaultTimeout
	}
	if validateDefinition(definition) != nil || len(prompt) > MaxInput || !utf8.ValidString(prompt) {
		result.Detail = "adviser configuration or prompt is invalid"
		return result
	}
	command, fileOutput := adviserCommand(definition, answerFile)
	var outputRoot *os.Root
	if fileOutput {
		var err error
		outputRoot, err = prepareAnswerFile(answerFile)
		if err != nil {
			result.Detail = "answer file must be a new absolute path in an existing directory"
			return result
		}
		defer func() { _ = outputRoot.Close() }()
	}
	program, err := adviserProgram(root, command[0])
	if err != nil {
		result.Detail = "adviser program could not be found or is not a supported executable"
		return result
	}
	ended := proc.Run(ctx, proc.Spec{
		Path: program, Args: command[1:], Dir: root, Env: adviserEnvironment(),
		Stdin: strings.NewReader(prompt), Timeout: definition.Timeout,
		StdoutLimit: MaxAnswer, StderrLimit: MaxDiagnostic,
	})
	// An ASCII replacement also keeps binary diagnostics within the byte cap.
	result.Diagnostics = strings.ToValidUTF8(string(ended.Stderr), "?")
	if ended.Code >= 0 {
		result.ExitStatus = &ended.Code
	}
	answer, overflow := ended.Stdout, ended.Overflow
	if fileOutput {
		answer, overflow, err = readAnswerFile(outputRoot, filepath.Base(answerFile))
	}
	result.Answer = validAnswerPrefix(answer)
	result.Outcome, result.Detail = executionOutcome(ctx, ended, answer, overflow, err)
	return result
}

func adviserCommand(definition Definition, answerFile string) ([]string, bool) {
	command := make([]string, len(definition.Command))
	fileOutput := false
	for i, arg := range definition.Command {
		switch arg {
		case "{model}":
			command[i] = definition.Model
		case "{answer_file}":
			command[i], fileOutput = answerFile, true
		default:
			command[i] = arg
		}
	}
	return command, fileOutput
}

func adviserEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "KITSU_ADVISER_CALL") {
			environment = append(environment, entry)
		}
	}
	return append(environment, "KITSU_ADVISER_CALL=1")
}

func adviserProgram(root, name string) (string, error) {
	if !filepath.IsAbs(name) && strings.ContainsAny(name, `/\`) {
		var err error
		name, err = filepath.Abs(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
	}
	program, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(program)) {
		case ".cmd", ".bat":
			return "", errors.New("batch launchers need a shell")
		}
	}
	return program, nil
}

func prepareAnswerFile(path string) (*os.Root, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("answer file must be absolute")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if _, err := root.Lstat(filepath.Base(path)); !errors.Is(err, fs.ErrNotExist) {
		_ = root.Close()
		return nil, errors.New("answer file already exists or cannot be inspected")
	}
	return root, nil
}

func readAnswerFile(root *os.Root, name string) ([]byte, bool, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false, errors.New("answer file is missing or is not a regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, false, errors.New("answer file changed or is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxAnswer+1))
	if len(data) > MaxAnswer {
		return data[:MaxAnswer], true, err
	}
	return data, false, err
}

func validAnswerPrefix(answer []byte) string {
	if utf8.Valid(answer) {
		return string(answer)
	}
	// Keep the exact valid prefix, including when the byte cap splits a rune.
	for offset := 0; offset < len(answer); {
		r, size := utf8.DecodeRune(answer[offset:])
		if r == utf8.RuneError && size == 1 {
			return string(answer[:offset])
		}
		offset += size
	}
	return ""
}

func executionOutcome(ctx context.Context, ended proc.Result, answer []byte, overflow bool, answerErr error) (string, string) {
	switch {
	case ended.Cancelled || (ended.Code != 0 && errors.Is(ctx.Err(), context.Canceled)):
		return "cancelled", "adviser call was cancelled"
	case ended.Stopped:
		return "timed_out", "adviser call exceeded its timeout"
	case ended.Cleanup != nil:
		return "failed", "adviser process cleanup did not complete"
	case ended.Overflow || overflow:
		return "overflow", "adviser output exceeded 1 MiB"
	case ended.Err != nil || ended.Code != 0:
		return "failed", "adviser program did not exit successfully"
	case answerErr != nil:
		return "failed", "adviser did not produce a readable regular answer file"
	case !utf8.Valid(answer):
		return "failed", "adviser answer is not valid UTF-8"
	case strings.TrimSpace(string(answer)) == "":
		return "failed", "adviser returned an empty answer"
	default:
		return "success", ""
	}
}
