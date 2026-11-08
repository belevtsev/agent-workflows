package credentials_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/v3/internal/credentials"
)

const (
	typesafeName    = "TYPESAFE_API_KEY"
	subqName        = "SUBQ_API_KEY"
	openaiName      = "OPENAI_API_KEY"
	credentialFile  = "credentials.json"
	lockFile        = ".credentials.lock"
	syntheticMarker = "synthetic-credential-boundary-marker"
	maxDocumentSize = 1 << 20
	maxValueSize    = 64 << 10
	operationBound  = 5 * time.Second
)

var providerNames = []string{typesafeName, subqName, openaiName}

var jsonEncoders = []struct {
	name   string
	encode func(any) ([]byte, error)
}{
	{name: "encoding/json", encode: json.Marshal},
	{name: "encoding/json/v2", encode: func(value any) ([]byte, error) { return jsonv2.Marshal(value) }},
}

func physicalDirectory(test *testing.T) string {
	test.Helper()
	directory, err := filepath.EvalSymlinks(test.TempDir())
	if err != nil {
		test.Fatalf("canonicalize fixture directory: %v", err)
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		test.Fatal("fixture directory is not an absolute clean physical path")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		test.Fatalf("make fixture directory private: %v", err)
	}
	return directory
}

func mustWriteFile(test *testing.T, filename string, document []byte) {
	test.Helper()
	if err := os.WriteFile(filename, document, 0o600); err != nil {
		test.Fatalf("write synthetic fixture: %v", err)
	}
	if err := os.Chmod(filename, 0o600); err != nil {
		test.Fatalf("make synthetic fixture private: %v", err)
	}
}

func documentFor(test *testing.T, keys map[string]string) []byte {
	test.Helper()
	document, err := json.Marshal(struct {
		Version int               `json:"version"`
		Keys    map[string]string `json:"keys"`
	}{Version: 1, Keys: keys})
	if err != nil {
		test.Fatalf("encode synthetic fixture: %v", err)
	}
	return document
}

func storeFixture(test *testing.T, keys map[string]string) credentials.Store {
	test.Helper()
	store := credentials.Store{Directory: physicalDirectory(test)}
	if keys != nil {
		mustWriteFile(test, filepath.Join(store.Directory, credentialFile), documentFor(test, keys))
	}
	return store
}

func mustReadFile(test *testing.T, filename string) []byte {
	test.Helper()
	document, err := os.ReadFile(filename)
	if err != nil {
		test.Fatalf("read synthetic fixture: %v", err)
	}
	return document
}

func mustStat(test *testing.T, filename string) os.FileInfo {
	test.Helper()
	info, err := os.Lstat(filename)
	if err != nil {
		test.Fatalf("stat synthetic fixture: %v", err)
	}
	return info
}

func generationFor(document []byte) string {
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:])
}

func pointerTo(value string) *string {
	return &value
}

func requireNoDisclosure(test *testing.T, output []byte, forbidden ...string) {
	test.Helper()
	forbidden = append(append([]string(nil), forbidden...), syntheticMarker)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		variants := [][]byte{[]byte(value)}
		for _, encoder := range jsonEncoders {
			encoded, err := encoder.encode(value)
			if err != nil {
				if !utf8.ValidString(value) {
					continue
				}
				test.Fatalf("encode disclosure oracle with %s: %v", encoder.name, err)
			}
			variants = append(variants, encoded, encoded[1:len(encoded)-1])
		}
		for _, variant := range variants {
			if bytes.Contains(output, variant) {
				test.Fatal("output discloses a synthetic credential or forbidden generation")
			}
		}
	}
}

func requireSafeError(test *testing.T, err error, forbidden ...string) {
	test.Helper()
	if err == nil {
		test.Fatal("expected an error")
	}
	requireNoDisclosure(test, []byte(err.Error()), forbidden...)
	for _, encoder := range jsonEncoders {
		encoded, marshalErr := encoder.encode(err)
		if marshalErr != nil {
			requireNoDisclosure(test, []byte(marshalErr.Error()), forbidden...)
			continue
		}
		requireNoDisclosure(test, encoded, forbidden...)
	}
}

func requireErrorIs(test *testing.T, err, sentinel error, forbidden ...string) {
	test.Helper()
	requireSafeError(test, err, forbidden...)
	if !errors.Is(err, sentinel) {
		test.Fatalf("error does not match required sentinel %v", sentinel)
	}
}

func requireSaveResult(test *testing.T, result credentials.SaveResult, changed, committed, durable bool, forbidden ...string) {
	test.Helper()
	if result.Changed != changed || result.Committed != committed || result.DurabilityVerified != durable {
		test.Fatalf("save flags = (%t, %t, %t), want (%t, %t, %t)", result.Changed, result.Committed, result.DurabilityVerified, changed, committed, durable)
	}
	want := map[string]bool{"changed": changed, "committed": committed, "durability_verified": durable}
	for _, encoder := range jsonEncoders {
		encoded, err := encoder.encode(result)
		if err != nil {
			test.Fatalf("marshal save result with %s: %v", encoder.name, err)
		}
		requireNoDisclosure(test, encoded, forbidden...)
		if bytes.Contains(encoded, []byte("null")) {
			test.Fatal("save result JSON must contain booleans, not null")
		}
		var fields map[string]bool
		if err := json.Unmarshal(encoded, &fields); err != nil {
			test.Fatalf("save result is not a boolean JSON object: %v", err)
		}
		if !reflect.DeepEqual(fields, want) {
			test.Fatalf("save result JSON has incorrect fields or values with %s", encoder.name)
		}
	}
}

func requireSnapshotJSON(test *testing.T, snapshot credentials.Snapshot) {
	test.Helper()
	forbidden := []string{snapshot.Generation}
	for _, value := range snapshot.Keys {
		forbidden = append(forbidden, value)
	}
	for _, encoder := range jsonEncoders {
		encoded, err := encoder.encode(snapshot)
		if err != nil {
			test.Fatalf("marshal snapshot with %s: %v", encoder.name, err)
		}
		requireNoDisclosure(test, encoded, forbidden...)
		if string(encoded) != "{}" {
			test.Fatalf("snapshot JSON must exclude both keys and generation with %s", encoder.name)
		}
	}
}

func requireEmptySnapshot(test *testing.T, snapshot credentials.Snapshot) {
	test.Helper()
	if snapshot.Keys == nil || len(snapshot.Keys) != 0 || snapshot.Generation != "" {
		test.Fatal("missing store must return a non-nil empty key map and empty generation")
	}
	requireSnapshotJSON(test, snapshot)
}

func decodeDocument(document []byte) (map[string]string, error) {
	if !utf8.Valid(document) || len(document) > maxDocumentSize {
		return nil, errors.New("invalid document encoding or size")
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("document is not an object")
	}
	seenFields := map[string]bool{}
	keys := map[string]string{}
	for decoder.More() {
		fieldToken, err := decoder.Token()
		field, isString := fieldToken.(string)
		if err != nil || !isString || seenFields[field] {
			return nil, errors.New("invalid or duplicate document field")
		}
		seenFields[field] = true
		switch field {
		case "version":
			var version int
			if err := decoder.Decode(&version); err != nil || version != 1 {
				return nil, errors.New("incorrect document version")
			}
		case "keys":
			opening, err := decoder.Token()
			if err != nil || opening != json.Delim('{') {
				return nil, errors.New("keys is not an object")
			}
			for decoder.More() {
				nameToken, err := decoder.Token()
				name, isString := nameToken.(string)
				if err != nil || !isString || (name != typesafeName && name != subqName && name != openaiName) {
					return nil, errors.New("unknown credential name")
				}
				if _, duplicate := keys[name]; duplicate {
					return nil, errors.New("duplicate credential name")
				}
				var value string
				if err := decoder.Decode(&value); err != nil || value == "" || len(value) > maxValueSize || !utf8.ValidString(value) {
					return nil, errors.New("invalid credential value")
				}
				for _, character := range value {
					if unicode.IsSpace(character) || unicode.IsControl(character) {
						return nil, errors.New("credential contains whitespace or control")
					}
				}
				keys[name] = value
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return nil, errors.New("unterminated keys object")
			}
		default:
			return nil, errors.New("unknown document field")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seenFields) != 2 || !seenFields["version"] || !seenFields["keys"] {
		return nil, errors.New("incomplete document schema")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing document content")
	}
	return keys, nil
}

func requireStored(test *testing.T, store credentials.Store, want map[string]string) credentials.Snapshot {
	test.Helper()
	snapshot, err := store.Read()
	if err != nil {
		test.Fatalf("read valid synthetic store: %v", err)
	}
	if snapshot.Keys == nil || !reflect.DeepEqual(snapshot.Keys, want) {
		test.Fatal("stored keys differ from the complete expected key map")
	}
	document := mustReadFile(test, filepath.Join(store.Directory, credentialFile))
	decoded, err := decodeDocument(document)
	if err != nil || !reflect.DeepEqual(decoded, want) {
		test.Fatal("persisted document does not have the exact version-1 allowlisted schema")
	}
	if snapshot.Generation != generationFor(document) {
		test.Fatal("generation is not lowercase SHA-256 of the exact current file bytes")
	}
	requireSnapshotJSON(test, snapshot)
	return snapshot
}

func requireUnchangedFile(test *testing.T, filename string, document []byte, before os.FileInfo) {
	test.Helper()
	if !bytes.Equal(mustReadFile(test, filename), document) {
		test.Fatal("credential bytes unexpectedly changed")
	}
	after := mustStat(test, filename)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		test.Fatal("credential file was replaced, rewritten, or had its mode changed")
	}
}

func requireNoCredentialFile(test *testing.T, directory string) {
	test.Helper()
	if _, err := os.Lstat(filepath.Join(directory, credentialFile)); !errors.Is(err, os.ErrNotExist) {
		test.Fatal("operation unexpectedly created a credential file")
	}
}

func requireOnlyStoreEntries(test *testing.T, directory string) {
	test.Helper()
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		test.Fatalf("list synthetic store: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != credentialFile && entry.Name() != lockFile {
			test.Fatal("store contains a backup, journal, stale staging file, or other unexpected artifact")
		}
	}
}

func requirePrivateDirectory(test *testing.T, directory string) {
	test.Helper()
	info := mustStat(test, directory)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		test.Fatal("store directory is not exactly private mode 0700")
	}
	details, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(details.Uid) != os.Geteuid() {
		test.Fatal("store directory has the wrong owner UID")
	}
}

func requirePrivateFile(test *testing.T, filename string) os.FileInfo {
	test.Helper()
	info := mustStat(test, filename)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		test.Fatal("store file is not a regular file with exact mode 0600")
	}
	details, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(details.Uid) != os.Geteuid() || details.Nlink != 1 {
		test.Fatal("store file has the wrong owner UID or more than one hard link")
	}
	return info
}

func boundedRead(test *testing.T, store credentials.Store) (credentials.Snapshot, error) {
	test.Helper()
	type outcome struct {
		snapshot credentials.Snapshot
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		snapshot, err := store.Read()
		completed <- outcome{snapshot: snapshot, err: err}
	}()
	timer := time.NewTimer(operationBound)
	defer timer.Stop()
	select {
	case result := <-completed:
		return result.snapshot, result.err
	case <-timer.C:
		test.Fatal("store read exceeded the five-second bound")
		return credentials.Snapshot{}, context.DeadlineExceeded
	}
}

func boundedSave(test *testing.T, ctx context.Context, store credentials.Store, changes map[string]*string, expected *string) (credentials.SaveResult, error) {
	test.Helper()
	type outcome struct {
		result credentials.SaveResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := store.Save(ctx, changes, expected)
		completed <- outcome{result: result, err: err}
	}()
	timer := time.NewTimer(operationBound)
	defer timer.Stop()
	select {
	case result := <-completed:
		return result.result, result.err
	case <-timer.C:
		test.Fatal("store save exceeded the five-second bound")
		return credentials.SaveResult{}, context.DeadlineExceeded
	}
}

func requireUnsafeStore(test *testing.T, store credentials.Store) {
	test.Helper()
	_, err := boundedRead(test, store)
	requireErrorIs(test, err, credentials.ErrUnsafeStore)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := boundedSave(test, ctx, store, map[string]*string{typesafeName: pointerTo(syntheticMarker + "-replacement")}, nil)
	requireErrorIs(test, err, credentials.ErrUnsafeStore)
	if result.Committed {
		test.Fatal("unsafe store save claimed a commit")
	}
}

func isolatedTestProcess(test *testing.T, childArgument string, environment ...string) bool {
	test.Helper()
	if len(os.Args) != 0 && os.Args[len(os.Args)-1] == childArgument {
		return true
	}
	executable, err := os.Executable()
	if err != nil {
		test.Fatalf("locate isolated test executable: %v", err)
	}
	directory := physicalDirectory(test)
	ctx, cancel := context.WithTimeout(context.Background(), operationBound)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^"+test.Name()+"$", "--", childArgument)
	command.Dir = directory
	command.Env = append([]string{
		"TMPDIR=" + directory,
		"HOME=" + directory,
		"XDG_CONFIG_HOME=" + directory,
	}, environment...)
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf("isolated assertion failed: %v\n%s", err, output)
	}
	return false
}

func TestRC1MissingStoreReadIsPure(test *testing.T) {
	for _, missingDirectory := range []bool{false, true} {
		test.Run(fmt.Sprintf("missing-directory-%t", missingDirectory), func(test *testing.T) {
			parent := physicalDirectory(test)
			directory := parent
			if missingDirectory {
				directory = filepath.Join(parent, "not-created")
			}
			snapshot, err := (credentials.Store{Directory: directory}).Read()
			if err != nil {
				test.Fatalf("read missing store: %v", err)
			}
			requireEmptySnapshot(test, snapshot)
			if missingDirectory {
				if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
					test.Fatal("read created a missing directory")
				}
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				test.Fatal("read of a missing store created filesystem artifacts")
			}
		})
	}
}

func TestRC1SaveSchemaModesAndGeneration(test *testing.T) {
	parent := physicalDirectory(test)
	store := credentials.Store{Directory: filepath.Join(parent, "private-store")}
	want := map[string]string{
		typesafeName: syntheticMarker + "-typesafe",
		subqName:     syntheticMarker + "-subq",
		openaiName:   syntheticMarker + "-openai",
	}
	changes := map[string]*string{}
	for name, value := range want {
		changes[name] = pointerTo(value)
	}
	result, err := store.Save(context.Background(), changes, pointerTo(""))
	if err != nil {
		test.Fatalf("save missing store: %v", err)
	}
	requireSaveResult(test, result, true, true, true)
	requirePrivateDirectory(test, store.Directory)
	requirePrivateFile(test, filepath.Join(store.Directory, credentialFile))
	requirePrivateFile(test, filepath.Join(store.Directory, lockFile))
	requireStored(test, store, want)
	requireOnlyStoreEntries(test, store.Directory)

	result, err = store.Save(context.Background(), map[string]*string{subqName: nil}, nil)
	if err != nil {
		test.Fatalf("remove stored key: %v", err)
	}
	requireSaveResult(test, result, true, true, true)
	delete(want, subqName)
	snapshot := requireStored(test, store, want)
	result, err = store.Save(context.Background(), map[string]*string{typesafeName: nil, openaiName: nil}, &snapshot.Generation)
	if err != nil {
		test.Fatalf("remove final stored keys: %v", err)
	}
	requireSaveResult(test, result, true, true, true)
	requireStored(test, store, map[string]string{})
	requireOnlyStoreEntries(test, store.Directory)
}

func TestRC1NoopDoesNotRewrite(test *testing.T) {
	value := syntheticMarker + "-unchanged"
	store := storeFixture(test, map[string]string{typesafeName: value})
	filename := filepath.Join(store.Directory, credentialFile)
	original := mustReadFile(test, filename)
	originalInfo := mustStat(test, filename)
	syncCalls := 0
	store.SyncDirectory = func(*os.File) error {
		syncCalls++
		return errors.New("noop must not sync")
	}
	cases := []struct {
		name    string
		changes map[string]*string
	}{
		{name: "nil-map"},
		{name: "empty-map", changes: map[string]*string{}},
		{name: "unchanged-value", changes: map[string]*string{typesafeName: &value}},
		{name: "remove-absent", changes: map[string]*string{subqName: nil}},
		{name: "unchanged-and-absent", changes: map[string]*string{typesafeName: &value, subqName: nil}},
	}
	for _, testcase := range cases {
		test.Run(testcase.name, func(test *testing.T) {
			expected := generationFor(original)
			result, err := store.Save(context.Background(), testcase.changes, &expected)
			if err != nil {
				test.Fatalf("save no-op: %v", err)
			}
			requireSaveResult(test, result, false, false, true, value, expected)
			requireUnchangedFile(test, filename, original, originalInfo)
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
	if syncCalls != 0 {
		test.Fatal("no-op called the post-rename sync seam")
	}
}

func TestRC1MissingRemovalDoesNotCreateCredentialFile(test *testing.T) {
	for _, changes := range []map[string]*string{nil, {}, {typesafeName: nil, subqName: nil, openaiName: nil}} {
		directory := filepath.Join(physicalDirectory(test), "missing-store")
		store := credentials.Store{Directory: directory}
		result, err := store.Save(context.Background(), changes, pointerTo(""))
		if err != nil {
			test.Fatalf("save missing-store no-op: %v", err)
		}
		requireSaveResult(test, result, false, false, true)
		requireNoCredentialFile(test, directory)
		requireOnlyStoreEntries(test, directory)
		if _, err := os.Lstat(directory); err == nil {
			requirePrivateDirectory(test, directory)
			if _, err := os.Lstat(filepath.Join(directory, lockFile)); err == nil {
				requirePrivateFile(test, filepath.Join(directory, lockFile))
			}
		}
		snapshot, err := store.Read()
		if err != nil {
			test.Fatalf("read after missing-store no-op: %v", err)
		}
		requireEmptySnapshot(test, snapshot)
	}
}

func TestRC2RejectsInvalidDocuments(test *testing.T) {
	secret := syntheticMarker + `-"\<>&-escaped`
	encodedSecret, err := json.Marshal(secret)
	if err != nil {
		test.Fatal(err)
	}
	value := string(encodedSecret)
	valid := `{"version":1,"keys":{"TYPESAFE_API_KEY":` + value + `}}`
	cases := []struct {
		name     string
		document []byte
	}{
		{name: "unknown-field", document: []byte(`{"version":1,"keys":{},"extra":` + value + `}`)},
		{name: "duplicate-version", document: []byte(`{"version":1,"version":1,"keys":{}}`)},
		{name: "duplicate-escaped-version", document: []byte(`{"version":1,"\u0076ersion":1,"keys":{}}`)},
		{name: "duplicate-keys", document: []byte(`{"version":1,"keys":{},"keys":{"TYPESAFE_API_KEY":` + value + `}}`)},
		{name: "duplicate-provider", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":` + value + `,"TYPESAFE_API_KEY":"other-synthetic"}}`)},
		{name: "duplicate-escaped-provider", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":` + value + `,"TYPESAFE_API_KE\u0059":"other-synthetic"}}`)},
		{name: "version-zero", document: []byte(`{"version":0,"keys":{}}`)},
		{name: "version-two", document: []byte(`{"version":2,"keys":{}}`)},
		{name: "version-string", document: []byte(`{"version":"1","keys":{}}`)},
		{name: "version-null", document: []byte(`{"version":null,"keys":{}}`)},
		{name: "version-bool", document: []byte(`{"version":true,"keys":{}}`)},
		{name: "missing-version", document: []byte(`{"keys":{}}`)},
		{name: "missing-keys", document: []byte(`{"version":1}`)},
		{name: "empty-object", document: []byte(`{}`)},
		{name: "top-null", document: []byte(`null`)},
		{name: "top-array", document: []byte(`[]`)},
		{name: "top-string", document: encodedSecret},
		{name: "top-bool", document: []byte(`true`)},
		{name: "top-number", document: []byte(`1`)},
		{name: "keys-null", document: []byte(`{"version":1,"keys":null}`)},
		{name: "keys-array", document: []byte(`{"version":1,"keys":[]}`)},
		{name: "keys-string", document: []byte(`{"version":1,"keys":` + value + `}`)},
		{name: "unknown-provider", document: []byte(`{"version":1,"keys":{"UNSUPPORTED_API_KEY":` + value + `}}`)},
		{name: "lowercase-provider", document: []byte(`{"version":1,"keys":{"typesafe_api_key":` + value + `}}`)},
		{name: "empty-provider", document: []byte(`{"version":1,"keys":{"":` + value + `}}`)},
		{name: "null-value", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":null}}`)},
		{name: "number-value", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":12}}`)},
		{name: "bool-value", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":true}}`)},
		{name: "object-value", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":{"secret":` + value + `}}}`)},
		{name: "array-value", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":[` + value + `]}}`)},
		{name: "empty-value", document: []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":""}}`)},
		{name: "leading-whitespace-value", document: documentFor(test, map[string]string{typesafeName: " " + secret})},
		{name: "trailing-whitespace-value", document: documentFor(test, map[string]string{typesafeName: secret + "\u3000"})},
		{name: "oversized-value", document: documentFor(test, map[string]string{typesafeName: syntheticMarker + strings.Repeat("x", maxValueSize)})},
		{name: "invalid-utf8", document: []byte("{\"version\":1,\"keys\":{\"TYPESAFE_API_KEY\":\"" + syntheticMarker + "-\xff\"}}")},
		{name: "invalid-utf8-field", document: []byte("{\"version\":1,\"keys\":{},\"" + syntheticMarker + "-\xff\":1}")},
		{name: "truncated", document: []byte(valid[:len(valid)-1])},
		{name: "trailing-junk", document: []byte(valid + "not-json")},
		{name: "second-document", document: []byte(valid + valid)},
		{name: "empty"},
		{name: "only-whitespace", document: []byte(" \r\n\t")},
		{name: "oversized-document", document: append([]byte(valid), bytes.Repeat([]byte(" "), maxDocumentSize-len(valid)+1)...)},
	}
	for _, testcase := range cases {
		test.Run(testcase.name, func(test *testing.T) {
			store := storeFixture(test, nil)
			filename := filepath.Join(store.Directory, credentialFile)
			mustWriteFile(test, filename, testcase.document)
			before := mustStat(test, filename)
			_, err := store.Read()
			requireErrorIs(test, err, credentials.ErrInvalidStore, secret)
			result, err := store.Save(context.Background(), map[string]*string{subqName: pointerTo(secret)}, nil)
			requireErrorIs(test, err, credentials.ErrInvalidStore, secret)
			if result.Committed {
				test.Fatal("save overwrote an invalid document")
			}
			requireUnchangedFile(test, filename, testcase.document, before)
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func TestRC2DocumentAndValueBoundaries(test *testing.T) {
	values := []struct {
		name  string
		value string
	}{
		{name: "escaped-punctuation-and-unicode", value: syntheticMarker + `-"\<>/&+=:é終`},
		{name: "one-byte", value: "x"},
		{name: "exact-64KiB-ascii", value: syntheticMarker + strings.Repeat("x", maxValueSize-len(syntheticMarker))},
		{name: "exact-64KiB-utf8-bytes", value: strings.Repeat("é", maxValueSize/2)},
	}
	for _, testcase := range values {
		test.Run(testcase.name, func(test *testing.T) {
			store := storeFixture(test, nil)
			result, err := store.Save(context.Background(), map[string]*string{typesafeName: &testcase.value}, nil)
			if err != nil {
				test.Fatalf("save valid boundary value: %v", err)
			}
			requireSaveResult(test, result, true, true, true, testcase.value)
			requireStored(test, store, map[string]string{typesafeName: testcase.value})
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
	test.Run("exact-1MiB-document", func(test *testing.T) {
		store := storeFixture(test, nil)
		want := map[string]string{typesafeName: syntheticMarker + "-document-boundary"}
		document := documentFor(test, want)
		document = append(document, bytes.Repeat([]byte(" "), maxDocumentSize-len(document))...)
		filename := filepath.Join(store.Directory, credentialFile)
		mustWriteFile(test, filename, document)
		before := mustStat(test, filename)
		requireStored(test, store, want)
		requireUnchangedFile(test, filename, document, before)
	})
}

func TestRC2RejectsInvalidChangesBeforeWrites(test *testing.T) {
	secret := syntheticMarker + `-"\<>&-invalid`
	cases := []struct {
		name    string
		changes map[string]*string
	}{
		{name: "unknown-name", changes: map[string]*string{"UNSUPPORTED_API_KEY": &secret}},
		{name: "unknown-name-removal", changes: map[string]*string{"UNSUPPORTED_API_KEY": nil}},
		{name: "empty-name", changes: map[string]*string{"": &secret}},
		{name: "wrong-case", changes: map[string]*string{"openai_api_key": &secret}},
		{name: "invalid-utf8-name", changes: map[string]*string{"INVALID_\xff": &secret}},
		{name: "empty-value", changes: map[string]*string{typesafeName: pointerTo("")}},
		{name: "leading-space", changes: map[string]*string{typesafeName: pointerTo(" " + secret)}},
		{name: "trailing-space", changes: map[string]*string{typesafeName: pointerTo(secret + " ")}},
		{name: "leading-unicode-space", changes: map[string]*string{typesafeName: pointerTo("\u3000" + secret)}},
		{name: "trailing-control", changes: map[string]*string{typesafeName: pointerTo(secret + "\x00")}},
		{name: "invalid-utf8-value", changes: map[string]*string{typesafeName: pointerTo(secret + "\xff")}},
		{name: "65KiB-value", changes: map[string]*string{typesafeName: pointerTo(secret + strings.Repeat("x", maxValueSize))}},
		{name: "64KiB-plus-one", changes: map[string]*string{typesafeName: pointerTo(strings.Repeat("x", maxValueSize+1))}},
		{name: "multibyte-byte-limit", changes: map[string]*string{typesafeName: pointerTo(strings.Repeat("é", maxValueSize/2) + "x")}},
		{name: "mixed-valid-and-invalid", changes: map[string]*string{typesafeName: &secret, subqName: nil, openaiName: pointerTo(secret + "\n")}},
	}
	for _, testcase := range cases {
		for _, existing := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/existing-%t", testcase.name, existing), func(test *testing.T) {
				store := storeFixture(test, nil)
				filename := filepath.Join(store.Directory, credentialFile)
				var original []byte
				var before os.FileInfo
				if existing {
					original = documentFor(test, map[string]string{typesafeName: secret, subqName: "synthetic-original-subq"})
					mustWriteFile(test, filename, original)
					before = mustStat(test, filename)
				}
				result, err := store.Save(context.Background(), testcase.changes, nil)
				forbidden := []string{secret}
				for _, value := range testcase.changes {
					if value != nil {
						forbidden = append(forbidden, *value)
					}
				}
				requireErrorIs(test, err, credentials.ErrInvalidChanges, forbidden...)
				if result.Committed {
					test.Fatal("invalid changes claimed a commit")
				}
				if existing {
					requireUnchangedFile(test, filename, original, before)
				} else {
					requireNoCredentialFile(test, store.Directory)
				}
				requireOnlyStoreEntries(test, store.Directory)
			})
		}
	}
}

func TestRC2RejectsUnicodeWhitespaceAndControls(test *testing.T) {
	for character := rune(0); character <= unicode.MaxRune; character++ {
		if !unicode.IsSpace(character) && !unicode.IsControl(character) {
			continue
		}
		test.Run(fmt.Sprintf("U+%04X", character), func(test *testing.T) {
			store := storeFixture(test, map[string]string{subqName: "synthetic-original"})
			filename := filepath.Join(store.Directory, credentialFile)
			original := mustReadFile(test, filename)
			before := mustStat(test, filename)
			value := syntheticMarker + "-left" + string(character) + "right"
			result, err := store.Save(context.Background(), map[string]*string{typesafeName: &value}, nil)
			requireErrorIs(test, err, credentials.ErrInvalidChanges, value)
			if result.Committed {
				test.Fatal("whitespace/control value was committed")
			}
			requireUnchangedFile(test, filename, original, before)
			requireOnlyStoreEntries(test, store.Directory)
			invalidDocument := documentFor(test, map[string]string{typesafeName: value})
			mustWriteFile(test, filename, invalidDocument)
			before = mustStat(test, filename)
			_, err = store.Read()
			requireErrorIs(test, err, credentials.ErrInvalidStore, value)
			result, err = store.Save(context.Background(), map[string]*string{openaiName: pointerTo("synthetic-valid")}, nil)
			requireErrorIs(test, err, credentials.ErrInvalidStore, value)
			if result.Committed {
				test.Fatal("save overwrote a whitespace/control document")
			}
			requireUnchangedFile(test, filename, invalidDocument, before)
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func TestRC2NamesCannotExpandAllowlist(test *testing.T) {
	if !reflect.DeepEqual(credentials.Names, providerNames) {
		test.Fatal("informational Names copy does not contain the three contracted names")
	}
	originalHeader := credentials.Names
	originalContents := append([]string(nil), originalHeader...)
	test.Cleanup(func() {
		copy(originalHeader, originalContents)
		credentials.Names = originalHeader
	})
	const injectedName = "INJECTED_SYNTHETIC_API_KEY"
	credentials.Names[0] = injectedName
	credentials.Names = append(credentials.Names, injectedName)
	store := storeFixture(test, nil)
	want := map[string]string{}
	changes := map[string]*string{}
	for _, name := range providerNames {
		value := syntheticMarker + "-" + name
		want[name] = value
		changes[name] = pointerTo(value)
	}
	result, err := store.Save(context.Background(), changes, nil)
	if err != nil {
		test.Fatalf("mutating Names changed the canonical allowlist: %v", err)
	}
	requireSaveResult(test, result, true, true, true)
	requireStored(test, store, want)
	filename := filepath.Join(store.Directory, credentialFile)
	original := mustReadFile(test, filename)
	before := mustStat(test, filename)
	for _, injectedValue := range []*string{pointerTo(syntheticMarker + "-injected"), nil} {
		result, err = store.Save(context.Background(), map[string]*string{injectedName: injectedValue}, nil)
		requireErrorIs(test, err, credentials.ErrInvalidChanges)
		if result.Committed {
			test.Fatal("mutated Names expanded the save allowlist")
		}
		requireUnchangedFile(test, filename, original, before)
	}
	resolver := credentials.Resolver{Directory: store.Directory, Getenv: func(name string) string {
		if name == injectedName {
			return "synthetic-unrelated-passthrough"
		}
		return ""
	}}
	getenv, err := resolver.Environment()
	if err != nil {
		test.Fatalf("resolve after Names mutation: %v", err)
	}
	for _, name := range providerNames {
		if getenv(name) != want[name] {
			test.Fatal("mutated Names removed a canonical resolver name")
		}
	}
	if getenv(injectedName) != "synthetic-unrelated-passthrough" {
		test.Fatal("mutated Names changed unrelated-name passthrough")
	}
	presence, err := resolver.Presence()
	if err != nil || len(presence) != len(providerNames) {
		test.Fatal("mutated Names changed presence allowlist")
	}
	for _, name := range providerNames {
		if item, exists := presence[name]; !exists || item.Source != "stored" || !item.Stored || !item.Effective || item.Environment {
			test.Fatal("mutated Names changed canonical presence")
		}
	}
	mustWriteFile(test, filename, documentFor(test, map[string]string{injectedName: syntheticMarker + "-injected"}))
	_, err = store.Read()
	requireErrorIs(test, err, credentials.ErrInvalidStore)
	requireOnlyStoreEntries(test, store.Directory)
}

func TestRC3RejectsNonphysicalPaths(test *testing.T) {
	if !isolatedTestProcess(test, "credentials-isolated-path-shape-child") {
		return
	}
	store := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-original"})
	filename := filepath.Join(store.Directory, credentialFile)
	original := mustReadFile(test, filename)
	before := mustStat(test, filename)
	for _, directory := range []string{
		"",
		"relative-synthetic-store",
		store.Directory + string(filepath.Separator),
		store.Directory + string(filepath.Separator) + ".",
		store.Directory + string(filepath.Separator) + "child/..",
	} {
		requireUnsafeStore(test, credentials.Store{Directory: directory})
	}
	requireUnchangedFile(test, filename, original, before)
	requireOnlyStoreEntries(test, store.Directory)
}

func TestRC3RejectsUnsafePathsAndFileTypes(test *testing.T) {
	test.Run("root-is-file", func(test *testing.T) {
		filename := filepath.Join(physicalDirectory(test), "not-a-directory")
		original := []byte(syntheticMarker + "-root-file")
		mustWriteFile(test, filename, original)
		before := mustStat(test, filename)
		requireUnsafeStore(test, credentials.Store{Directory: filename})
		requireUnchangedFile(test, filename, original, before)
	})
	for _, kind := range []string{"root-symlink", "ancestor-symlink", "file-symlink", "dangling-root-symlink", "dangling-file-symlink", "hardlink", "directory", "fifo"} {
		test.Run(kind, func(test *testing.T) {
			store := storeFixture(test, nil)
			target := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-external-target"})
			targetPath := filepath.Join(target.Directory, credentialFile)
			targetBytes := mustReadFile(test, targetPath)
			targetInfo := mustStat(test, targetPath)
			filename := filepath.Join(store.Directory, credentialFile)
			var fixtureErr error
			switch kind {
			case "root-symlink":
				alias := filepath.Join(physicalDirectory(test), "alias")
				fixtureErr = os.Symlink(target.Directory, alias)
				store.Directory = alias
			case "ancestor-symlink":
				child := filepath.Join(target.Directory, "child")
				if err := os.Mkdir(child, 0o700); err != nil {
					test.Fatal(err)
				}
				mustWriteFile(test, filepath.Join(child, credentialFile), targetBytes)
				alias := filepath.Join(physicalDirectory(test), "ancestor")
				fixtureErr = os.Symlink(target.Directory, alias)
				store.Directory = filepath.Join(alias, "child")
				targetPath = filepath.Join(child, credentialFile)
				targetInfo = mustStat(test, targetPath)
			case "file-symlink":
				fixtureErr = os.Symlink(targetPath, filename)
			case "dangling-root-symlink":
				alias := filepath.Join(physicalDirectory(test), "dangling")
				fixtureErr = os.Symlink(filepath.Join(target.Directory, "absent"), alias)
				store.Directory = alias
			case "dangling-file-symlink":
				fixtureErr = os.Symlink(filepath.Join(target.Directory, "absent"), filename)
			case "hardlink":
				fixtureErr = os.Link(targetPath, filename)
			case "directory":
				fixtureErr = os.Mkdir(filename, 0o700)
			case "fifo":
				fixtureErr = syscall.Mkfifo(filename, 0o600)
			}
			if fixtureErr != nil {
				test.Fatalf("create unsafe synthetic fixture: %v", fixtureErr)
			}
			unsafeInfo := mustStat(test, store.Directory)
			if kind != "root-symlink" && kind != "ancestor-symlink" && kind != "dangling-root-symlink" {
				unsafeInfo = mustStat(test, filename)
			}
			requireUnsafeStore(test, store)
			requireUnchangedFile(test, targetPath, targetBytes, targetInfo)
			targetEntries, err := os.ReadDir(filepath.Dir(targetPath))
			if err != nil || len(targetEntries) != 1 || targetEntries[0].Name() != credentialFile {
				test.Fatal("rejected store followed an external target to create artifacts")
			}
			checkPath := filename
			if kind == "root-symlink" || kind == "ancestor-symlink" || kind == "dangling-root-symlink" {
				checkPath = store.Directory
			}
			after := mustStat(test, checkPath)
			if !os.SameFile(unsafeInfo, after) || unsafeInfo.Mode() != after.Mode() {
				test.Fatal("rejected unsafe filesystem object was replaced or modified")
			}
			if kind == "file-symlink" || kind == "dangling-file-symlink" {
				if _, err := os.Readlink(filename); err != nil {
					test.Fatal("rejected credential symlink was removed")
				}
			}
			if kind != "root-symlink" && kind != "ancestor-symlink" && kind != "dangling-root-symlink" {
				requireOnlyStoreEntries(test, store.Directory)
			}
		})
	}
}

func TestRC3RejectsUnsafePermissions(test *testing.T) {
	cases := []struct {
		name      string
		directory bool
		mode      os.FileMode
	}{
		{name: "directory-zero", directory: true, mode: 0},
		{name: "directory-0500", directory: true, mode: 0o500},
		{name: "directory-0710", directory: true, mode: 0o710},
		{name: "directory-0755", directory: true, mode: 0o755},
		{name: "directory-0770", directory: true, mode: 0o770},
		{name: "directory-sticky", directory: true, mode: 0o700 | os.ModeSticky},
		{name: "file-zero", mode: 0},
		{name: "file-0400", mode: 0o400},
		{name: "file-0640", mode: 0o640},
		{name: "file-0644", mode: 0o644},
		{name: "file-0660", mode: 0o660},
		{name: "file-setuid", mode: 0o600 | os.ModeSetuid},
	}
	for _, testcase := range cases {
		test.Run(testcase.name, func(test *testing.T) {
			store := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-private"})
			filename := filepath.Join(store.Directory, credentialFile)
			original := mustReadFile(test, filename)
			before := mustStat(test, filename)
			changedPath := filename
			restoreMode := os.FileMode(0o600)
			if testcase.directory {
				changedPath = store.Directory
				restoreMode = 0o700
			}
			if err := os.Chmod(changedPath, testcase.mode); err != nil {
				test.Fatalf("set unsafe fixture permissions: %v", err)
			}
			test.Cleanup(func() { _ = os.Chmod(changedPath, restoreMode) })
			if mustStat(test, changedPath).Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != testcase.mode {
				test.Fatal("filesystem did not create the required unsafe-permission fixture")
			}
			requireUnsafeStore(test, store)
			if mustStat(test, changedPath).Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != testcase.mode {
				test.Fatal("rejection silently repaired unsafe permissions")
			}
			if err := os.Chmod(changedPath, restoreMode); err != nil {
				test.Fatal(err)
			}
			requireUnchangedFile(test, filename, original, before)
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func TestRC3RejectsForeignOwners(test *testing.T) {
	for _, kind := range []string{"directory", "credential-file", "lock-file"} {
		test.Run(kind, func(test *testing.T) {
			store := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-owner"})
			filename := filepath.Join(store.Directory, credentialFile)
			original := mustReadFile(test, filename)
			before := mustStat(test, filename)
			changedPath := filename
			if kind == "directory" {
				changedPath = store.Directory
			} else if kind == "lock-file" {
				changedPath = filepath.Join(store.Directory, lockFile)
				mustWriteFile(test, changedPath, nil)
			}
			originalUID := os.Geteuid()
			foreignUID := 0
			if originalUID == 0 {
				foreignUID = 1
			}
			if err := os.Chown(changedPath, foreignUID, -1); err != nil {
				if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
					test.Skip("foreign-UID fixture requires permission to chown; boundary remains required")
				}
				test.Fatalf("create foreign-owner fixture: %v", err)
			}
			test.Cleanup(func() { _ = os.Chown(changedPath, originalUID, -1) })
			if kind == "lock-file" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				result, err := boundedSave(test, ctx, store, map[string]*string{subqName: pointerTo("synthetic-added")}, nil)
				requireErrorIs(test, err, credentials.ErrUnsafeStore)
				if result.Committed {
					test.Fatal("foreign-owner lock admitted a commit")
				}
			} else {
				requireUnsafeStore(test, store)
			}
			details, ok := mustStat(test, changedPath).Sys().(*syscall.Stat_t)
			if !ok || int(details.Uid) != foreignUID {
				test.Fatal("rejection silently repaired foreign ownership")
			}
			if err := os.Chown(changedPath, originalUID, -1); err != nil {
				test.Fatal(err)
			}
			requireUnchangedFile(test, filename, original, before)
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func TestRC3RejectsUnsafeLockFiles(test *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "directory", "fifo", "mode-0644"} {
		test.Run(kind, func(test *testing.T) {
			store := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-original"})
			filename := filepath.Join(store.Directory, credentialFile)
			original := mustReadFile(test, filename)
			before := mustStat(test, filename)
			targetPath := filepath.Join(physicalDirectory(test), "external-lock-target")
			targetBytes := []byte(syntheticMarker + "-external-lock")
			mustWriteFile(test, targetPath, targetBytes)
			targetInfo := mustStat(test, targetPath)
			lockPath := filepath.Join(store.Directory, lockFile)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(targetPath, lockPath)
			case "hardlink":
				err = os.Link(targetPath, lockPath)
			case "directory":
				err = os.Mkdir(lockPath, 0o700)
			case "fifo":
				err = syscall.Mkfifo(lockPath, 0o600)
			case "mode-0644":
				mustWriteFile(test, lockPath, nil)
				err = os.Chmod(lockPath, 0o644)
			}
			if err != nil {
				test.Fatalf("create unsafe lock fixture: %v", err)
			}
			lockInfo := mustStat(test, lockPath)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := boundedSave(test, ctx, store, map[string]*string{subqName: pointerTo(syntheticMarker + "-new")}, nil)
			requireErrorIs(test, err, credentials.ErrUnsafeStore)
			if result.Committed {
				test.Fatal("unsafe lock admitted a commit")
			}
			requireUnchangedFile(test, filename, original, before)
			requireUnchangedFile(test, targetPath, targetBytes, targetInfo)
			if after := mustStat(test, lockPath); !os.SameFile(lockInfo, after) || lockInfo.Mode() != after.Mode() {
				test.Fatal("rejected stable lock was replaced or modified")
			}
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func TestRC4CompareAndSwapUsesExactBytes(test *testing.T) {
	for _, existing := range []bool{false, true} {
		test.Run(fmt.Sprintf("existing-%t", existing), func(test *testing.T) {
			store := storeFixture(test, nil)
			filename := filepath.Join(store.Directory, credentialFile)
			var original []byte
			var before os.FileInfo
			if existing {
				original = []byte(" {\n  \"keys\": {\"TYPESAFE_API_KEY\": \"synthetic-original\"},\n  \"version\": 1\n}\n")
				mustWriteFile(test, filename, original)
				before = mustStat(test, filename)
				snapshot := requireStored(test, store, map[string]string{typesafeName: "synthetic-original"})
				if snapshot.Generation != generationFor(original) {
					test.Fatal("generation canonicalized rather than hashing original bytes")
				}
			}
			wrongGenerations := []string{strings.Repeat("0", 64), "not-a-current-generation"}
			if existing {
				wrongGenerations = append(wrongGenerations, "", generationFor(documentFor(test, map[string]string{typesafeName: "synthetic-original"})))
				wrongGenerations = append(wrongGenerations, " "+generationFor(original)+" ")
				if uppercase := strings.ToUpper(generationFor(original)); uppercase != generationFor(original) {
					wrongGenerations = append(wrongGenerations, uppercase)
				}
			}
			for _, expected := range wrongGenerations {
				for _, changes := range []map[string]*string{{subqName: pointerTo(syntheticMarker + "-new")}, {}} {
					result, err := store.Save(context.Background(), changes, &expected)
					requireErrorIs(test, err, credentials.ErrConflict)
					if result.Committed {
						test.Fatal("conflict claimed a commit")
					}
					if existing {
						requireUnchangedFile(test, filename, original, before)
					} else {
						requireNoCredentialFile(test, store.Directory)
					}
					requireOnlyStoreEntries(test, store.Directory)
				}
			}
			expected := ""
			want := map[string]string{subqName: syntheticMarker + "-new"}
			if existing {
				expected = generationFor(original)
				want[typesafeName] = "synthetic-original"
			}
			result, err := store.Save(context.Background(), map[string]*string{subqName: pointerTo(want[subqName])}, &expected)
			if err != nil {
				test.Fatalf("save with exact generation: %v", err)
			}
			requireSaveResult(test, result, true, true, true)
			requireStored(test, store, want)
			if existing && os.SameFile(before, mustStat(test, filename)) {
				test.Fatal("changed credential file was not atomically replaced")
			}
			want[openaiName] = "synthetic-unchecked"
			result, err = store.Save(context.Background(), map[string]*string{openaiName: pointerTo(want[openaiName])}, nil)
			if err != nil {
				test.Fatalf("save with unchecked nil generation: %v", err)
			}
			requireSaveResult(test, result, true, true, true)
			requireStored(test, store, want)
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func TestRC4ConcurrentCASIsAtomic(test *testing.T) {
	initial := map[string]string{typesafeName: "synthetic-initial-typesafe", subqName: "synthetic-initial-subq", openaiName: "synthetic-preserved"}
	store := storeFixture(test, initial)
	filename := filepath.Join(store.Directory, credentialFile)
	expected := generationFor(mustReadFile(test, filename))
	const writerCount = 12
	candidates := make([]map[string]string, writerCount)
	for index := range candidates {
		candidates[index] = map[string]string{
			typesafeName: fmt.Sprintf("synthetic-writer-%02d-typesafe", index),
			subqName:     fmt.Sprintf("synthetic-writer-%02d-subq", index),
			openaiName:   initial[openaiName],
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationBound)
	defer cancel()
	stopObserver := make(chan struct{})
	observerStarted := make(chan struct{})
	observerDone := make(chan error, 1)
	go func() {
		first := true
		for {
			document, err := os.ReadFile(filename)
			if err != nil {
				observerDone <- errors.New("atomic observer could not read the existing credential file")
				return
			}
			keys, err := decodeDocument(document)
			if err != nil {
				observerDone <- errors.New("atomic observer saw a truncated or invalid document")
				return
			}
			complete := reflect.DeepEqual(keys, initial)
			for _, candidate := range candidates {
				complete = complete || reflect.DeepEqual(keys, candidate)
			}
			if !complete {
				observerDone <- errors.New("atomic observer saw mixed writer fields")
				return
			}
			if first {
				close(observerStarted)
				first = false
			}
			select {
			case <-stopObserver:
				observerDone <- nil
				return
			case <-ctx.Done():
				observerDone <- errors.New("atomic observer exceeded its bound")
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	select {
	case <-observerStarted:
	case err := <-observerDone:
		test.Fatalf("start atomic observer: %v", err)
	case <-ctx.Done():
		test.Fatal("atomic observer did not start within its bound")
	}
	type outcome struct {
		index  int
		result credentials.SaveResult
		err    error
	}
	start := make(chan struct{})
	completed := make(chan outcome, writerCount)
	for index, candidate := range candidates {
		go func(index int, candidate map[string]string) {
			<-start
			result, err := store.Save(ctx, map[string]*string{typesafeName: pointerTo(candidate[typesafeName]), subqName: pointerTo(candidate[subqName])}, &expected)
			completed <- outcome{index: index, result: result, err: err}
		}(index, candidate)
	}
	close(start)
	winner := -1
	for count := 0; count < writerCount; count++ {
		select {
		case outcome := <-completed:
			if outcome.err == nil {
				if winner != -1 {
					test.Fatal("more than one concurrent writer succeeded with the same generation")
				}
				winner = outcome.index
				requireSaveResult(test, outcome.result, true, true, true)
			} else {
				requireErrorIs(test, outcome.err, credentials.ErrConflict)
				if outcome.result.Committed {
					test.Fatal("conflicting concurrent writer claimed a commit")
				}
			}
		case <-ctx.Done():
			test.Fatal("concurrent CAS writers exceeded their five-second bound")
		}
	}
	close(stopObserver)
	select {
	case err := <-observerDone:
		if err != nil {
			test.Fatal(err)
		}
	case <-ctx.Done():
		test.Fatal("atomic observer did not finish within its bound")
	}
	if winner == -1 {
		test.Fatal("no valid concurrent CAS writer succeeded")
	}
	requireStored(test, store, candidates[winner])
	requirePrivateFile(test, filename)
	requirePrivateFile(test, filepath.Join(store.Directory, lockFile))
	requireOnlyStoreEntries(test, store.Directory)
}

func TestRC4PrecancelledSaveDoesNotCommit(test *testing.T) {
	for _, existing := range []bool{false, true} {
		test.Run(fmt.Sprintf("existing-%t", existing), func(test *testing.T) {
			store := storeFixture(test, nil)
			filename := filepath.Join(store.Directory, credentialFile)
			var original []byte
			var before os.FileInfo
			if existing {
				original = documentFor(test, map[string]string{typesafeName: syntheticMarker + "-original"})
				mustWriteFile(test, filename, original)
				before = mustStat(test, filename)
			}
			syncCalls := 0
			store.SyncDirectory = func(*os.File) error { syncCalls++; return nil }
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			result, err := boundedSave(test, ctx, store, map[string]*string{typesafeName: pointerTo(syntheticMarker + "-replacement")}, nil)
			requireSafeError(test, err)
			if result.Committed || syncCalls != 0 {
				test.Fatal("pre-cancelled save committed or invoked a post-rename hook")
			}
			if existing {
				requireUnchangedFile(test, filename, original, before)
			} else {
				requireNoCredentialFile(test, store.Directory)
			}
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func acquireFixtureLock(test *testing.T, directory string) (*os.File, func()) {
	test.Helper()
	lock, err := os.OpenFile(filepath.Join(directory, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		test.Fatalf("open fixture lock: %v", err)
	}
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		test.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		test.Fatalf("hold fixture lock: %v", err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}
	test.Cleanup(release)
	return lock, release
}

func TestRC4WriterLockHonorsContextAndRemainsStable(test *testing.T) {
	store := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-original"})
	filename := filepath.Join(store.Directory, credentialFile)
	original := mustReadFile(test, filename)
	before := mustStat(test, filename)
	_, release := acquireFixtureLock(test, store.Directory)
	lockPath := filepath.Join(store.Directory, lockFile)
	lockInfo := requirePrivateFile(test, lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	result, err := boundedSave(test, ctx, store, map[string]*string{subqName: pointerTo(syntheticMarker + "-waiting")}, nil)
	requireSafeError(test, err)
	if !errors.Is(err, credentials.ErrBusy) && !errors.Is(err, context.DeadlineExceeded) {
		test.Fatal("lock wait did not report busy or the expired context")
	}
	if result.Committed {
		test.Fatal("writer committed while a different descriptor held the stable lock")
	}
	requireUnchangedFile(test, filename, original, before)
	if !os.SameFile(lockInfo, mustStat(test, lockPath)) {
		test.Fatal("contending writer replaced or removed the stable lock")
	}
	requireOnlyStoreEntries(test, store.Directory)
	release()
	expected := generationFor(original)
	result, err = store.Save(context.Background(), map[string]*string{subqName: pointerTo("synthetic-after-unlock")}, &expected)
	if err != nil {
		test.Fatalf("save after releasing fixture lock: %v", err)
	}
	requireSaveResult(test, result, true, true, true)
	requireStored(test, store, map[string]string{typesafeName: syntheticMarker + "-original", subqName: "synthetic-after-unlock"})
	if !os.SameFile(lockInfo, requirePrivateFile(test, lockPath)) {
		test.Fatal("successful writer replaced the stable lock inode")
	}
	requireOnlyStoreEntries(test, store.Directory)
}

func TestRC3HeldLockPathRebindingDoesNotFollowTarget(test *testing.T) {
	parent := physicalDirectory(test)
	directory := filepath.Join(parent, "store")
	if err := os.Mkdir(directory, 0o700); err != nil {
		test.Fatal(err)
	}
	oldKeys := map[string]string{typesafeName: "synthetic-original-root"}
	original := documentFor(test, oldKeys)
	mustWriteFile(test, filepath.Join(directory, credentialFile), original)
	store := credentials.Store{Directory: directory}
	rootInfo := mustStat(test, directory)
	syncCalls := 0
	var hookViolation error
	store.SyncDirectory = func(root *os.File) error {
		syncCalls++
		if root == nil {
			hookViolation = errors.New("rebound sync hook received no directory descriptor")
			return hookViolation
		}
		info, err := root.Stat()
		if err != nil || !info.IsDir() || !os.SameFile(rootInfo, info) {
			hookViolation = errors.New("rebound sync hook followed the replacement path instead of the anchored directory")
			return hookViolation
		}
		return nil
	}
	target := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-external-root"})
	targetPath := filepath.Join(target.Directory, credentialFile)
	targetBytes := mustReadFile(test, targetPath)
	targetInfo := mustStat(test, targetPath)
	_, release := acquireFixtureLock(test, directory)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type outcome struct {
		result credentials.SaveResult
		err    error
	}
	started := make(chan struct{})
	completed := make(chan outcome, 1)
	go func() {
		close(started)
		result, err := store.Save(ctx, map[string]*string{subqName: pointerTo("synthetic-rebound-writer")}, nil)
		completed <- outcome{result: result, err: err}
	}()
	<-started
	time.Sleep(20 * time.Millisecond)
	parked := filepath.Join(parent, "parked-original")
	if err := os.Rename(directory, parked); err != nil {
		test.Fatal(err)
	}
	if err := os.Symlink(target.Directory, directory); err != nil {
		test.Fatal(err)
	}
	release()
	timer := time.NewTimer(operationBound)
	defer timer.Stop()
	var outcomeValue outcome
	select {
	case outcomeValue = <-completed:
	case <-timer.C:
		test.Fatal("path-rebound writer exceeded its five-second bound")
	}
	requireUnchangedFile(test, targetPath, targetBytes, targetInfo)
	entries, err := os.ReadDir(target.Directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != credentialFile {
		test.Fatal("path-rebound writer followed the external target to create artifacts")
	}
	if _, err := os.Readlink(directory); err != nil {
		test.Fatal("path-rebound writer replaced the attacker symlink")
	}
	if outcomeValue.err != nil {
		requireSafeError(test, outcomeValue.err)
	}
	if hookViolation != nil {
		test.Fatal(hookViolation)
	}
	if outcomeValue.result.Committed {
		if syncCalls != 1 {
			test.Fatal("committed rebound writer did not sync the anchored directory exactly once")
		}
		requireStored(test, credentials.Store{Directory: parked}, map[string]string{typesafeName: oldKeys[typesafeName], subqName: "synthetic-rebound-writer"})
	} else {
		if syncCalls != 0 {
			test.Fatal("uncommitted rebound writer invoked the post-rename sync seam")
		}
		if outcomeValue.err == nil {
			test.Fatal("changed path-rebound save neither committed nor reported an error")
		}
		if !bytes.Equal(mustReadFile(test, filepath.Join(parked, credentialFile)), original) {
			test.Fatal("uncommitted path-rebound save changed the original document")
		}
	}
	requireOnlyStoreEntries(test, parked)
}

func TestRC5PostrenameSyncFailureIsCommittedAndSafe(test *testing.T) {
	var fixedError string
	for _, suffix := range []string{"first", "different-second"} {
		test.Run(suffix, func(test *testing.T) {
			store := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-old"})
			filename := filepath.Join(store.Directory, credentialFile)
			original := mustReadFile(test, filename)
			before := mustStat(test, filename)
			expected := generationFor(original)
			rootInfo := mustStat(test, store.Directory)
			newValue := syntheticMarker + `-"\<>&-new-` + suffix
			want := map[string]string{typesafeName: newValue}
			injected := errors.New(syntheticMarker + "-private-sync-error-" + suffix)
			calls := 0
			var hookViolation error
			store.SyncDirectory = func(directory *os.File) error {
				calls++
				if directory == nil {
					hookViolation = errors.New("post-rename hook received no directory descriptor")
					return injected
				}
				info, err := directory.Stat()
				if err != nil || !info.IsDir() || !os.SameFile(info, rootInfo) {
					hookViolation = errors.New("post-rename hook did not receive the anchored store directory")
				}
				document, err := os.ReadFile(filename)
				keys, decodeErr := decodeDocument(document)
				if err != nil || decodeErr != nil || !reflect.DeepEqual(keys, want) {
					hookViolation = errors.New("sync seam ran before updated credentials were visible")
				}
				after, err := os.Lstat(filename)
				if err != nil || os.SameFile(before, after) {
					hookViolation = errors.New("sync seam ran before atomic file replacement")
				}
				return injected
			}
			result, err := store.Save(context.Background(), map[string]*string{typesafeName: &newValue}, &expected)
			requireErrorIs(test, err, credentials.ErrPersistenceUnconfirmed, newValue, injected.Error())
			requireSaveResult(test, result, true, true, false, newValue, expected)
			if calls != 1 || hookViolation != nil {
				test.Fatalf("post-rename sync ordering or call count violated: calls=%d, violation=%v", calls, hookViolation)
			}
			if fixedError == "" {
				fixedError = err.Error()
			} else if err.Error() != fixedError {
				test.Fatal("persistence-unconfirmed error is not a safe fixed error")
			}
			lowerError := strings.ToLower(err.Error())
			if strings.Contains(lowerError, "unsaved") || strings.Contains(lowerError, "rolled back") || strings.Contains(lowerError, "rollback") {
				test.Fatal("post-rename error falsely claims the committed update was unsaved or rolled back")
			}
			reloaded := requireStored(test, store, want)
			committedBytes := mustReadFile(test, filename)
			committedInfo := mustStat(test, filename)
			if reloaded.Generation == expected {
				test.Fatal("committed replacement did not change generation")
			}
			result, err = store.Save(context.Background(), map[string]*string{typesafeName: &newValue}, &expected)
			requireErrorIs(test, err, credentials.ErrConflict, newValue)
			if result.Committed || calls != 1 {
				test.Fatal("stale retry after uncertain durability committed or re-ran the sync hook")
			}
			requireUnchangedFile(test, filename, committedBytes, committedInfo)
			result, err = store.Save(context.Background(), map[string]*string{typesafeName: &newValue}, &reloaded.Generation)
			if err != nil {
				test.Fatalf("retry unchanged value after reload: %v", err)
			}
			requireSaveResult(test, result, false, false, true, newValue, reloaded.Generation)
			requireUnchangedFile(test, filename, committedBytes, committedInfo)
			if calls != 1 {
				test.Fatal("reload no-op retry invoked the post-rename hook")
			}
			store.SyncDirectory = nil
			want[subqName] = "synthetic-normal-durable-update"
			result, err = store.Save(context.Background(), map[string]*string{subqName: pointerTo(want[subqName])}, &reloaded.Generation)
			if err != nil {
				test.Fatalf("normal save after reloading committed state: %v", err)
			}
			requireSaveResult(test, result, true, true, true)
			requireStored(test, store, want)
			requirePrivateFile(test, filename)
			requirePrivateFile(test, filepath.Join(store.Directory, lockFile))
			requireOnlyStoreEntries(test, store.Directory)
		})
	}
}

func TestRC5SuccessfulPostrenameSyncHook(test *testing.T) {
	store := storeFixture(test, map[string]string{typesafeName: "synthetic-original"})
	filename := filepath.Join(store.Directory, credentialFile)
	before := mustStat(test, filename)
	rootInfo := mustStat(test, store.Directory)
	want := map[string]string{typesafeName: syntheticMarker + "-successful-sync"}
	calls := 0
	var hookViolation error
	store.SyncDirectory = func(directory *os.File) error {
		calls++
		if directory == nil {
			hookViolation = errors.New("successful sync hook received no descriptor")
			return hookViolation
		}
		info, err := directory.Stat()
		if err != nil || !info.IsDir() || !os.SameFile(rootInfo, info) {
			hookViolation = errors.New("successful sync hook received the wrong directory")
			return hookViolation
		}
		document, err := os.ReadFile(filename)
		keys, decodeErr := decodeDocument(document)
		after, statErr := os.Lstat(filename)
		if err != nil || decodeErr != nil || statErr != nil || !reflect.DeepEqual(keys, want) || os.SameFile(before, after) {
			hookViolation = errors.New("successful sync hook ran before atomic replacement")
			return hookViolation
		}
		return nil
	}
	result, err := store.Save(context.Background(), map[string]*string{typesafeName: pointerTo(want[typesafeName])}, nil)
	if err != nil || calls != 1 || hookViolation != nil {
		test.Fatalf("successful post-rename sync failed: calls=%d, error=%v, violation=%v", calls, err, hookViolation)
	}
	requireSaveResult(test, result, true, true, true)
	requireStored(test, store, want)
	requireOnlyStoreEntries(test, store.Directory)
}

func requirePresenceJSON(test *testing.T, presence map[string]credentials.Presence, forbidden ...string) {
	test.Helper()
	for _, encoder := range jsonEncoders {
		encoded, err := encoder.encode(presence)
		if err != nil {
			test.Fatalf("marshal presence with %s: %v", encoder.name, err)
		}
		requireNoDisclosure(test, encoded, forbidden...)
		var fields map[string]map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != len(providerNames) {
			test.Fatal("presence JSON does not contain exactly the three provider names")
		}
		for _, name := range providerNames {
			item, exists := fields[name]
			if !exists || len(item) != 4 {
				test.Fatal("presence JSON has incorrect provider fields")
			}
			want := presence[name]
			for field, wantValue := range map[string]bool{"environment": want.Environment, "stored": want.Stored, "effective": want.Effective} {
				var actual bool
				if encodedValue, exists := item[field]; !exists || json.Unmarshal(encodedValue, &actual) != nil || actual != wantValue || string(encodedValue) == "null" {
					test.Fatal("presence JSON has an incorrect boolean field")
				}
			}
			var source string
			if encodedValue, exists := item["source"]; !exists || json.Unmarshal(encodedValue, &source) != nil || source != want.Source {
				test.Fatal("presence JSON has an incorrect source field")
			}
		}
	}
}

func TestRC6PrecedencePresenceAndReadOnlyResolution(test *testing.T) {
	for pattern := 0; pattern < 64; pattern++ {
		test.Run(fmt.Sprintf("presence-pattern-%02d", pattern), func(test *testing.T) {
			stored := map[string]string{}
			environment := map[string]string{
				"SYNTHETIC_UNRELATED":   " synthetic-unrelated\tvalue ",
				"OPENAI_API_KEY_SUFFIX": "synthetic-unrelated-suffix",
				"typesafe_api_key":      "synthetic-unrelated-lowercase",
			}
			for index, name := range providerNames {
				if pattern&(1<<(index*2)) != 0 {
					environment[name] = syntheticMarker + "-environment-" + name
				}
				if pattern&(1<<(index*2+1)) != 0 {
					stored[name] = syntheticMarker + "-stored-" + name
				}
			}
			store := storeFixture(test, nil)
			filename := filepath.Join(store.Directory, credentialFile)
			var original []byte
			var before os.FileInfo
			if len(stored) != 0 {
				original = documentFor(test, stored)
				mustWriteFile(test, filename, original)
				before = mustStat(test, filename)
			}
			resolver := credentials.Resolver{Directory: store.Directory, Getenv: func(name string) string { return environment[name] }}
			getenv, err := resolver.Environment()
			if err != nil || getenv == nil {
				test.Fatalf("resolve valid environment/store pattern: %v", err)
			}
			presence, err := resolver.Presence()
			if err != nil || len(presence) != len(providerNames) {
				test.Fatalf("inspect valid presence pattern: %v", err)
			}
			for _, name := range providerNames {
				envPresent := environment[name] != ""
				storedPresent := stored[name] != ""
				wantValue := stored[name]
				wantSource := "none"
				if storedPresent {
					wantSource = "stored"
				}
				if envPresent {
					wantValue = environment[name]
					wantSource = "environment"
				}
				if getenv(name) != wantValue {
					test.Fatal("resolver did not apply per-provider environment precedence")
				}
				item, exists := presence[name]
				if !exists || item.Environment != envPresent || item.Stored != storedPresent || item.Effective != (envPresent || storedPresent) || item.Source != wantSource {
					test.Fatal("presence booleans or source do not reflect independent environment and stored presence")
				}
			}
			for _, name := range []string{"SYNTHETIC_UNRELATED", "OPENAI_API_KEY_SUFFIX", "typesafe_api_key", "SYNTHETIC_ABSENT_UNRELATED"} {
				if getenv(name) != environment[name] {
					test.Fatal("resolver substituted an unrelated name instead of calling the supplied getenv")
				}
			}
			forbidden := []string{}
			for _, value := range stored {
				forbidden = append(forbidden, value)
			}
			for _, value := range environment {
				forbidden = append(forbidden, value)
			}
			if len(stored) != 0 {
				forbidden = append(forbidden, generationFor(original))
				requireUnchangedFile(test, filename, original, before)
			} else {
				requireNoCredentialFile(test, store.Directory)
			}
			requirePresenceJSON(test, presence, forbidden...)
			entries, err := os.ReadDir(store.Directory)
			wantEntries := 0
			if len(stored) != 0 {
				wantEntries = 1
			}
			if err != nil || len(entries) != wantEntries {
				test.Fatal("environment or presence inspection wrote filesystem artifacts")
			}
		})
	}
}

func TestRC6EnvironmentBypassesButPresenceInspectsBadStore(test *testing.T) {
	for _, kind := range []string{"malformed", "root-symlink", "ancestor-symlink", "file-symlink", "unsafe-directory-mode", "unsafe-file-mode"} {
		test.Run(kind, func(test *testing.T) {
			store := storeFixture(test, map[string]string{typesafeName: syntheticMarker + "-stored"})
			filename := filepath.Join(store.Directory, credentialFile)
			directory := store.Directory
			sentinel := credentials.ErrUnsafeStore
			switch kind {
			case "malformed":
				mustWriteFile(test, filename, []byte(`{"version":1,"keys":{"TYPESAFE_API_KEY":"`+syntheticMarker+`"},"unknown":true}`))
				sentinel = credentials.ErrInvalidStore
			case "root-symlink":
				alias := filepath.Join(physicalDirectory(test), "alias")
				if err := os.Symlink(directory, alias); err != nil {
					test.Fatal(err)
				}
				directory = alias
			case "ancestor-symlink":
				alias := filepath.Join(physicalDirectory(test), "alias")
				if err := os.Symlink(filepath.Dir(directory), alias); err != nil {
					test.Fatal(err)
				}
				directory = filepath.Join(alias, filepath.Base(directory))
			case "file-symlink":
				targetPath := filepath.Join(physicalDirectory(test), "external-credentials")
				if err := os.Rename(filename, targetPath); err != nil {
					test.Fatal(err)
				}
				if err := os.Symlink(targetPath, filename); err != nil {
					test.Fatal(err)
				}
			case "unsafe-directory-mode":
				if err := os.Chmod(directory, 0o755); err != nil {
					test.Fatal(err)
				}
			case "unsafe-file-mode":
				if err := os.Chmod(filename, 0o644); err != nil {
					test.Fatal(err)
				}
			}
			original := mustReadFile(test, filename)
			before := mustStat(test, filename)
			environment := map[string]string{}
			for _, name := range providerNames {
				environment[name] = syntheticMarker + "-environment-" + name
			}
			resolver := credentials.Resolver{Directory: directory, Getenv: func(name string) string { return environment[name] }}
			getenv, err := resolver.Environment()
			if err != nil || getenv == nil {
				test.Fatalf("complete environment must bypass unsafe or malformed fallback disk: %v", err)
			}
			for _, name := range providerNames {
				if getenv(name) != environment[name] {
					test.Fatal("complete environment lookup did not preserve supplied values")
				}
			}
			_, err = resolver.Presence()
			requireErrorIs(test, err, sentinel)
			delete(environment, openaiName)
			_, err = resolver.Environment()
			requireErrorIs(test, err, sentinel)
			requireUnchangedFile(test, filename, original, before)
			entries, err := os.ReadDir(store.Directory)
			if err != nil || len(entries) != 1 || entries[0].Name() != credentialFile {
				test.Fatal("resolver wrote to an unsafe or malformed store")
			}
		})
	}
}

func TestRC6MissingDirectoryIsNeverCreated(test *testing.T) {
	for _, fullEnvironment := range []bool{false, true} {
		test.Run(fmt.Sprintf("full-environment-%t", fullEnvironment), func(test *testing.T) {
			directory := filepath.Join(physicalDirectory(test), "missing-store")
			environment := map[string]string{}
			if fullEnvironment {
				for _, name := range providerNames {
					environment[name] = syntheticMarker + "-environment-" + name
				}
			}
			resolver := credentials.Resolver{Directory: directory, Getenv: func(name string) string { return environment[name] }}
			getenv, err := resolver.Environment()
			if err != nil || getenv == nil {
				test.Fatalf("resolve missing store: %v", err)
			}
			presence, err := resolver.Presence()
			if err != nil || len(presence) != len(providerNames) {
				test.Fatalf("inspect missing store: %v", err)
			}
			for _, name := range providerNames {
				if getenv(name) != environment[name] {
					test.Fatal("missing-store resolver returned an unexpected value")
				}
				item, exists := presence[name]
				wantSource := "none"
				if fullEnvironment {
					wantSource = "environment"
				}
				if !exists || item.Environment != fullEnvironment || item.Stored || item.Effective != fullEnvironment || item.Source != wantSource {
					test.Fatal("missing-store presence has incorrect source or booleans")
				}
			}
			requirePresenceJSON(test, presence)
			if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
				test.Fatal("resolver or presence inspection created a missing directory")
			}
		})
	}
}

func TestRC6ResolverDoesNotMutateProcessEnvironment(test *testing.T) {
	if !isolatedTestProcess(test, "credentials-isolated-process-environment-child",
		typesafeName+"=synthetic-process-typesafe",
		openaiName+"=synthetic-process-openai",
		"SYNTHETIC_UNRELATED=synthetic-process-unrelated",
	) {
		return
	}
	store := storeFixture(test, map[string]string{
		typesafeName: syntheticMarker + "-stored-typesafe",
		subqName:     syntheticMarker + "-stored-subq",
		openaiName:   syntheticMarker + "-stored-openai",
	})
	resolver := credentials.Resolver{Directory: store.Directory, Getenv: func(name string) string {
		if name == typesafeName {
			return syntheticMarker + "-injected-environment"
		}
		return ""
	}}
	getenv, err := resolver.Environment()
	if err != nil || getenv == nil {
		test.Fatalf("resolve inside isolated process: %v", err)
	}
	for _, name := range providerNames {
		if getenv(name) == "" {
			test.Fatal("isolated resolver did not supply a synthetic key")
		}
	}
	if _, err := resolver.Presence(); err != nil {
		test.Fatalf("inspect presence inside isolated process: %v", err)
	}
	for name, want := range map[string]string{
		typesafeName:          "synthetic-process-typesafe",
		openaiName:            "synthetic-process-openai",
		"SYNTHETIC_UNRELATED": "synthetic-process-unrelated",
	} {
		if actual, exists := os.LookupEnv(name); !exists || actual != want {
			test.Fatal("resolver mutated a pinned synthetic process environment variable")
		}
	}
	if _, exists := os.LookupEnv(subqName); exists {
		test.Fatal("resolver globally installed a stored key into the process environment")
	}
}
